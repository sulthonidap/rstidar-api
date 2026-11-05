package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
	"medsend-api/internal/services"
)

// fakeFiles records the storage keys that were deleted.
type fakeFiles struct{ deleted []string }

func (f *fakeFiles) Save(context.Context, string, io.Reader, int64, string) error { return nil }
func (f *fakeFiles) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}
func (f *fakeFiles) URL(key string) string { return key }

func TestDestroyOrder(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Role{}, &models.Organization{}, &models.User{}, &models.Courier{},
		&models.Service{}, &models.ServiceTier{}, &models.Customer{}, &models.Order{}, &models.OrderStatusHistory{},
		&models.Transaction{}, &models.OrderDelivery{}, &models.CourierRoute{}, &models.CourierRouteStop{}, &models.Ward{}); err != nil {
		t.Fatal(err)
	}
	admin := models.User{Name: "Admin", Email: "admin@test", Password: "x", IsActive: true}
	db.Create(&admin)

	newOrder := func() models.Order {
		code, err := services.GenerateOrderCode(db)
		if err != nil {
			t.Fatal(err)
		}
		o := models.Order{Code: code, CustomerName: "Budi", CustomerPhone: "0812", PickupAddress: "RS", DropoffAddress: "Rumah"}
		if err := db.Create(&o).Error; err != nil {
			t.Fatal(err)
		}
		return o
	}
	first, second, third := newOrder(), newOrder(), newOrder()

	// Everything hanging off the order that gets deleted, plus a neighbour's row that must stay.
	proof := "receipts/1.jpg"
	db.Create(&models.Transaction{OrderID: second.ID, Proof: &proof})
	db.Create(&models.Transaction{OrderID: third.ID})
	db.Create(&models.OrderStatusHistory{OrderID: second.ID, ToStatus: enums.OrderStatusPending, ChangedAt: time.Now()})
	db.Create(&models.OrderDelivery{
		OrderID: second.ID, ReceiverName: "Budi", ReceiverRelation: "pasien", DeliveredAt: time.Now(),
		Photos: []string{"pod/1.jpg"}, Thumbs: []string{"pod/1-t.jpg"},
	})
	route := models.CourierRoute{CourierID: 1, StartedAt: time.Now()}
	db.Create(&route)
	db.Create(&models.CourierRouteStop{RouteID: route.ID, OrderID: second.ID, Seq: 1})

	files := &fakeFiles{}
	h := NewOrderHandler(db, nil, files, nil)
	app := fiber.New()
	app.Delete("/api/orders/:id", func(c *fiber.Ctx) error {
		c.Locals(middleware.UserContextKey, &admin)
		return c.Next()
	}, h.Destroy)
	del := func(id string, body map[string]any) (int, string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("DELETE", "/api/orders/"+id, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(out)
	}
	count := func(model any, id uint) (n int64) {
		db.Model(model).Where("order_id = ?", id).Count(&n)
		return
	}

	// No or wrong confirm: refused, nothing deleted, and the response names the order.
	if code, out := del("2", nil); code != 422 || !strings.Contains(out, second.Code) {
		t.Fatalf("no confirm must be refused with the code: %d %s", code, out)
	}
	if code, _ := del("2", map[string]any{"confirm": first.Code}); code != 422 {
		t.Fatalf("another order's code must be refused, got %d", code)
	}
	if code, _ := del("999", map[string]any{"confirm": "x"}); code != 404 {
		t.Fatalf("unknown order must be 404, got %d", code)
	}
	var orders int64
	if db.Model(&models.Order{}).Count(&orders); orders != 3 {
		t.Fatalf("a refused delete must keep the orders, got %d", orders)
	}

	// Confirmed: the order and everything under it are gone, files too.
	if code, out := del("2", map[string]any{"confirm": second.Code}); code != 200 {
		t.Fatalf("delete: %d %s", code, out)
	}
	if db.Model(&models.Order{}).Where("id = ?", second.ID).Count(&orders); orders != 0 {
		t.Fatal("order must be deleted")
	}
	for _, m := range []any{&models.Transaction{}, &models.OrderStatusHistory{}, &models.OrderDelivery{}, &models.CourierRouteStop{}} {
		if n := count(m, second.ID); n != 0 {
			t.Fatalf("%T left behind: %d", m, n)
		}
	}
	if n := count(&models.Transaction{}, third.ID); n != 1 {
		t.Fatal("another order's transaction must stay")
	}
	if got := strings.Join(files.deleted, ","); got != "pod/1.jpg,pod/1-t.jpg,receipts/1.jpg" {
		t.Fatalf("files deleted: %s", got)
	}

	// A gap in today's codes must not make the next code collide.
	fourth := newOrder()
	if !strings.HasSuffix(fourth.Code, "-0004") {
		t.Fatalf("next code must follow the highest, got %s", fourth.Code)
	}

	// By order code as well.
	if code, out := del(first.Code, map[string]any{"confirm": first.Code}); code != 200 {
		t.Fatalf("delete by code: %d %s", code, out)
	}
}
