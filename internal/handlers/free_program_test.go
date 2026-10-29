package handlers

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strconv"
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
		&models.OrderStatusHistory{}, &models.Transaction{}, &models.OrderDelivery{}, &models.AppSetting{}, &models.Ward{}, &models.Region{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Organization{Name: "RSUD Tidar"})
	seedRegions(db)
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
			"payment_method": "cash", "dropoff_region_code": "33.71.01.1010", "patient_category": category, "domicile_checked": checked,
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

// Separate switches: the staff form (enabled) and the public form & kiosk
// (public). A patient's own pick is a claim CCO verifies by Edit.
func TestFreeProgramSwitches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Role{}, &models.Organization{}, &models.User{}, &models.Courier{},
		&models.Service{}, &models.ServiceTier{}, &models.Customer{}, &models.Order{},
		&models.OrderStatusHistory{}, &models.Transaction{}, &models.OrderDelivery{}, &models.AppSetting{}, &models.Ward{}, &models.Region{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&models.Organization{Name: "RSUD Tidar"})
	seedRegions(db)
	svc := models.Service{Name: "Reguler", PriceBase: 12000, IsActive: true}
	db.Create(&svc)
	cco := models.User{Name: "CCO", Email: "cco@test", Password: "x", IsActive: true}
	db.Create(&cco)

	h := NewOrderHandler(db, nil, nil, nil)
	pub := &PublicHandler{DB: db}
	settings := &SettingsHandler{DB: db}
	app := fiber.New()
	app.Post("/public/orders", pub.StoreOrder)
	app.Get("/public/free-program", settings.PublicFreeProgram)
	api := app.Group("/api", func(c *fiber.Ctx) error {
		c.Locals(middleware.UserContextKey, &cco)
		return c.Next()
	})
	api.Post("/orders", h.Store)
	api.Put("/orders/:id", h.Update)
	api.Post("/orders/:id/free-claim", h.FreeClaim)

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
	body := func(extra map[string]any) map[string]any {
		b := map[string]any{
			"service_id": svc.ID, "customer_name": "Budi", "customer_phone": "081234567890",
			"dropoff_address": "Jl. Pemuda 12, Magelang", "patient_name": "Budi", "medical_record_no": "RM1",
			"payment_method": "cash", "dropoff_region_code": "33.71.01.1010",
			"care_type": "rawat_jalan", "pharmacy_queue_no": "A-001",
		}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	last := func() models.Order {
		var o models.Order
		db.Order("id DESC").First(&o)
		return o
	}
	staffFree := map[string]any{"patient_category": "lansia", "domicile_checked": true}

	// Only the public switch on.
	db.Create(&models.AppSetting{Name: setFreePublic, Value: ptr("1")})
	if _, out := call("GET", "/public/free-program", nil); !strings.Contains(out, `"enabled":true`) {
		t.Fatalf("public form must see the program: %s", out)
	}
	// The staff form is off: its category is ignored.
	if code, out := call("POST", "/api/orders", body(staffFree)); code != 201 {
		t.Fatalf("staff create: %d %s", code, out)
	}
	if o := last(); o.IsFree || o.PatientCategory != enums.PatientUmum {
		t.Fatalf("staff switch off must ignore the category: %+v", o)
	}
	// A patient's claim (even saying checked) stays unverified and pays.
	if code, out := call("POST", "/public/orders", body(map[string]any{"patient_category": "disabilitas", "domicile_checked": true})); code != 201 {
		t.Fatalf("public create: %d %s", code, out)
	}
	claim := last()
	if claim.IsFree || claim.PatientCategory != enums.PatientDisabilitas || claim.DomicileChecked || claim.Price != 12000 {
		t.Fatalf("a claim must wait for CCO: %+v", claim)
	}
	var hist models.OrderStatusHistory
	db.Where("order_id = ?", claim.ID).First(&hist)
	if hist.Note == nil || !strings.Contains(*hist.Note, "mengajukan ongkir gratis (Disabilitas)") {
		t.Fatalf("history must flag the claim: %v", hist.Note)
	}
	claimURL := func(id uint) string { return "/api/orders/" + strconv.Itoa(int(id)) + "/free-claim" }
	// CCO checks the KTP and approves → free, settled at Rp0.
	if code, out := call("POST", claimURL(claim.ID), map[string]any{"action": "approve"}); code != 200 {
		t.Fatalf("approve: %d %s", code, out)
	}
	if o := last(); !o.IsFree || o.Price != 0 || !o.DomicileChecked || o.FreeVerifiedBy == nil {
		t.Fatalf("approved claim must be free: %+v", o)
	}
	var txn models.Transaction
	db.Where("order_id = ?", claim.ID).First(&txn)
	if txn.PaymentStatus != enums.PaymentStatusPaid || txn.Amount != 0 {
		t.Fatalf("approved claim must be settled at Rp0: %+v", txn)
	}
	// Decided already: no second decision.
	if code, _ := call("POST", claimURL(claim.ID), map[string]any{"action": "reject"}); code != 422 {
		t.Fatalf("a decided claim must be refused, got %d", code)
	}

	// Rejected (with a reason): a regular order, still to pay, reason in the history.
	call("POST", "/public/orders", body(map[string]any{"patient_category": "lansia"}))
	rejected := last()
	if code, out := call("POST", claimURL(rejected.ID), map[string]any{"action": "reject", "note": "KTP Kab. Semarang"}); code != 200 {
		t.Fatalf("reject: %d %s", code, out)
	}
	if o := last(); o.IsFree || o.PatientCategory != enums.PatientUmum || o.Price != 12000 {
		t.Fatalf("rejected claim must be a regular order: %+v", o)
	}
	var rejNote models.OrderStatusHistory
	db.Where("order_id = ?", rejected.ID).Order("id DESC").First(&rejNote)
	if rejNote.Note == nil || *rejNote.Note != "Pengajuan ongkir gratis (Lansia) ditolak. Catatan: KTP Kab. Semarang" {
		t.Fatalf("history must record the rejection: %v", rejNote.Note)
	}

	// Public switch off: the category from the public form is ignored.
	db.Model(&models.AppSetting{}).Where("name = ?", setFreePublic).Update("value", "0")
	call("POST", "/public/orders", body(map[string]any{"patient_category": "lansia"}))
	if o := last(); o.PatientCategory != enums.PatientUmum {
		t.Fatalf("public switch off must ignore the category: %+v", o)
	}
}
