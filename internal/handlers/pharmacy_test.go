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

func TestOrderPharmacy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Role{}, &models.Organization{}, &models.User{}, &models.Courier{},
		&models.Service{}, &models.ServiceTier{}, &models.Customer{}, &models.Order{}, &models.Ward{}, &models.Region{},
		&models.OrderStatusHistory{}, &models.Transaction{}, &models.OrderDelivery{}, &models.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Organization{Name: "RSUD Tidar"})
	seedRegions(db)
	svc := models.Service{Name: "Reguler", PriceBase: 12000, IsActive: true}
	db.Create(&svc)
	dahlia := models.Ward{Name: "Dahlia 1", IsActive: true}
	db.Create(&dahlia)
	closed := models.Ward{Name: "Aster 2"}
	db.Create(&closed)
	db.Model(&closed).Update("is_active", false)
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

	call := func(method, path string, extra map[string]any) (int, string) {
		t.Helper()
		body := map[string]any{
			"service_id": svc.ID, "customer_name": "Budi", "customer_phone": "081234567890",
			"dropoff_address": "Jl. Pemuda 12, Magelang", "patient_name": "Budi", "medical_record_no": "RM1",
			"payment_method": "cash", "dropoff_region_code": "33.71.01.1010",
		}
		for k, v := range extra {
			body[k] = v
		}
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
	load := func(id uint) models.Order {
		var o models.Order
		db.First(&o, id)
		return o
	}

	refused := []map[string]any{
		{},                           // farmasi not picked
		{"care_type": "igd"},         // unknown
		{"care_type": "rawat_jalan"}, // no queue number
		{"care_type": "rawat_inap"},  // no ward
		{"care_type": "rawat_inap", "ward_id": closed.ID}, // inactive ward
	}
	for _, extra := range refused {
		if code, _ := call("POST", "/api/orders", extra); code != fiber.StatusUnprocessableEntity {
			t.Fatalf("%v must be refused, got %d", extra, code)
		}
	}

	// Rawat jalan + queue number.
	if code, out := call("POST", "/api/orders", map[string]any{"care_type": "rawat_jalan", "pharmacy_queue_no": " A-012 "}); code != 201 {
		t.Fatalf("create failed: %d %s", code, out)
	}
	if o := load(1); o.CareType == nil || *o.CareType != enums.CareRawatJalan || derefString(o.PharmacyQueueNo) != "A-012" || o.WardID != nil {
		t.Fatalf("rawat jalan not saved: %+v", o)
	}

	// Edit to rawat inap: the queue number goes, the ward is set, history says "farmasi".
	code, out := call("PUT", "/api/orders/1", map[string]any{"care_type": "rawat_inap", "ward_id": dahlia.ID, "pharmacy_queue_no": "A-012"})
	if code != 200 || !strings.Contains(out, `"farmasi"`) {
		t.Fatalf("edit failed: %d %s", code, out)
	}
	if o := load(1); *o.CareType != enums.CareRawatInap || o.PharmacyQueueNo != nil || derefUint(o.WardID) != dahlia.ID {
		t.Fatalf("rawat inap not saved: %+v", o)
	}

	// Edit without farmasi (old client) keeps it; the ward, once inactive, can be kept.
	db.Model(&dahlia).Update("is_active", false)
	if code, out := call("PUT", "/api/orders/1", map[string]any{"care_type": "rawat_inap", "ward_id": dahlia.ID}); code != 200 {
		t.Fatalf("keeping an inactive ward must work: %d %s", code, out)
	}
	if code, _ := call("PUT", "/api/orders/1", nil); code != 200 {
		t.Fatal("edit without farmasi must work")
	}
	if o := load(1); o.CareType == nil || derefUint(o.WardID) != dahlia.ID {
		t.Fatalf("farmasi must be kept: %+v", o)
	}
}

func TestPublicOrderPharmacyToggle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Organization{}, &models.Service{}, &models.ServiceTier{}, &models.Customer{},
		&models.Order{}, &models.Ward{}, &models.Region{}, &models.OrderStatusHistory{}, &models.Transaction{}, &models.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Organization{Name: "RSUD Tidar"})
	seedRegions(db)
	svc := models.Service{Name: "Reguler", PriceBase: 12000, IsActive: true}
	db.Create(&svc)
	ward := models.Ward{Name: "ICU", IsActive: true}
	db.Create(&ward)

	h := &PublicHandler{DB: db}
	app := fiber.New()
	app.Post("/orders", h.StoreOrder)
	post := func(extra map[string]any) int {
		t.Helper()
		body := map[string]any{
			"service_id": svc.ID, "customer_name": "Budi", "customer_phone": "081234567890",
			"dropoff_address": "Jl. Pemuda 12, Magelang", "patient_name": "Budi", "medical_record_no": "RM1",
			"payment_method": "cash", "dropoff_region_code": "33.71.01.1010",
		}
		for k, v := range extra {
			body[k] = v
		}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/orders", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode
	}

	// Off (default): no farmasi needed, and none is saved.
	if code := post(map[string]any{"care_type": "rawat_jalan", "pharmacy_queue_no": "A-1"}); code != 201 {
		t.Fatalf("toggle off: got %d", code)
	}
	var o models.Order
	db.First(&o)
	if o.CareType != nil {
		t.Fatal("farmasi must be ignored while the toggle is off")
	}

	// On: required.
	db.Create(&models.AppSetting{Name: setPharmacyPublic, Value: ptr("1")})
	if code := post(nil); code != fiber.StatusUnprocessableEntity {
		t.Fatalf("toggle on without farmasi: got %d", code)
	}
	if code := post(map[string]any{"care_type": "rawat_inap", "ward_id": ward.ID}); code != 201 {
		t.Fatalf("toggle on with ward: got %d", code)
	}
}
