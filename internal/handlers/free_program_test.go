package handlers

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

func TestFreeDeliveryProgram(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Role{}, &models.Organization{}, &models.User{}, &models.Courier{},
		&models.Service{}, &models.ServiceTier{}, &models.Customer{}, &models.Order{},
		&models.OrderStatusHistory{}, &models.Transaction{}, &models.OrderDelivery{}, &models.AppSetting{}, &models.Ward{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Organization{Name: "RSUD Tidar"})
	svc := models.Service{Name: "Reguler", PriceBase: 12000, IsActive: true}
	db.Create(&svc)
	cco := models.User{Name: "CCO", Email: "cco@test", Password: "x", IsActive: true}
	db.Create(&cco)

	h := NewOrderHandler(db, nil, nil, nil)
	app := fiber.New()
	api := app.Group("/api", func(c *fiber.Ctx) error {
		c.Locals(middleware.UserContextKey, &cco)
		return c.Next()
	})
	api.Post("/orders", h.Store)
	api.Put("/orders/:id", h.Update)

	call := func(method, path string, body map[string]any) (int, string) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		out, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(out)
	}
	orderBody := func(category string, checked bool) map[string]any {
		return map[string]any{
			"service_id": svc.ID, "customer_name": "Budi", "customer_phone": "081234567890",
			"dropoff_address": "Jl. Pemuda 12, Magelang", "patient_name": "Budi", "medical_record_no": "RM1",
			"payment_method": "cash", "patient_category": category, "domicile_checked": checked,
			"care_type": "rawat_jalan", "pharmacy_queue_no": "A-001",
		}
	}
	load := func(id uint) (models.Order, models.Transaction) {
		var o models.Order
		var txn models.Transaction
		db.First(&o, id)
		db.Where("order_id = ?", id).First(&txn)
		return o, txn
	}

	// Program off: a verified lansia still pays.
	if code, out := call("POST", "/api/orders", orderBody("lansia", true)); code != 201 && code != 200 {
		t.Fatalf("create failed: %d %s", code, out)
	}
	if o, _ := load(1); o.IsFree || o.Price != 12000 {
		t.Fatalf("program off must not be free: %+v", o)
	}

	// Program on: verified lansia → Rp0, normal price kept, paid automatically.
	db.Create(&models.AppSetting{Name: setFreeEnabled, Value: ptr("1")})
	call("POST", "/api/orders", orderBody("lansia", true))
	o, txn := load(2)
	if !o.IsFree || o.Price != 0 || o.NormalPrice == nil || *o.NormalPrice != 12000 || o.FreeVerifiedBy == nil {
		t.Fatalf("verified lansia must be free with normal price kept: %+v", o)
	}
	if txn.PaymentStatus != enums.PaymentStatusPaid || txn.Amount != 0 {
		t.Fatalf("free order must be settled at Rp0: %+v", txn)
	}

	// Domicile not checked → pays.
	call("POST", "/api/orders", orderBody("disabilitas", false))
	if o, _ := load(3); o.IsFree {
		t.Fatal("unchecked domicile must not be free")
	}

	// Edit back to "umum" → not free, transaction back to unpaid at the tariff.
	if code, out := call("PUT", "/api/orders/2", orderBody("umum", false)); code != 200 {
		t.Fatalf("edit failed: %d %s", code, out)
	}
	o, txn = load(2)
	if o.IsFree || o.Price != 12000 || txn.PaymentStatus != enums.PaymentStatusUnpaid || txn.Amount != 12000 {
		t.Fatalf("un-free order must be payable again: %+v %+v", o, txn)
	}

	// Patient already paid, then marked free → refund first.
	db.Model(&models.Transaction{}).Where("order_id = ?", 2).Update("payment_status", enums.PaymentStatusPaid)
	if code, _ := call("PUT", "/api/orders/2", orderBody("lansia", true)); code != fiber.StatusUnprocessableEntity {
		t.Fatalf("marking a patient-paid order free must ask for a refund, got %d", code)
	}
}

func ptr(s string) *string { return &s }
