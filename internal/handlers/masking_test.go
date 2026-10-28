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

// The panel gets masked patient data; edits that send it back unchanged keep
// the real values; labels and the courier app get the real data.
func TestPatientDataMasking(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Role{}, &models.Organization{}, &models.User{}, &models.Courier{},
		&models.Service{}, &models.ServiceTier{}, &models.Customer{}, &models.Order{},
		&models.OrderStatusHistory{}, &models.Transaction{}, &models.OrderDelivery{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "RSUD Tidar"}
	db.Create(&org)
	svc := models.Service{Name: "Reguler", PriceBase: 15000, IsActive: true}
	db.Create(&svc)
	admin := models.User{Name: "Admin", Email: "admin@test", Password: "x", IsActive: true}
	db.Create(&admin)
	patient, mrn := "Siti Aminah", "00123456"
	order := models.Order{Code: "MS-1", OrganizationID: &org.ID, ServiceID: &svc.ID, Status: enums.OrderStatusPending,
		CustomerName: "Budi Santoso", CustomerPhone: "081234567890", PatientName: &patient, MedicalRecordNo: &mrn,
		PickupAddress: "RSUD Tidar", DropoffAddress: "Jl. Pemuda 12, Magelang", Price: 15000}
	db.Create(&order)

	h := NewOrderHandler(db, nil, nil, nil)
	app := fiber.New()
	api := app.Group("/api", func(c *fiber.Ctx) error {
		c.Locals(middleware.UserContextKey, &admin)
		return c.Next()
	}, middleware.MaskPatientData())
	api.Get("/orders/:id", h.Show)
	api.Put("/orders/:id", h.Update)
	api.Post("/orders/labels", h.Labels)
	api.Get("/courier/orders/:id", h.Show) // stands in for the courier app (exempt path)

	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return res.StatusCode, out
	}
	field := func(out map[string]any, key string) string {
		d, _ := out["data"].(map[string]any)
		s, _ := d[key].(string)
		return s
	}

	_, shown := call("GET", "/api/orders/1", "")
	if field(shown, "customer_name") != "B*** S******" || field(shown, "medical_record_no") != "******56" {
		t.Fatalf("panel must get masked data, got %v / %v", field(shown, "customer_name"), field(shown, "medical_record_no"))
	}

	// Edit form sends the masked values back untouched + a real change in notes.
	body, _ := json.Marshal(map[string]any{
		"service_id": svc.ID, "customer_name": field(shown, "customer_name"), "customer_phone": field(shown, "customer_phone"),
		"dropoff_address": field(shown, "dropoff_address"), "notes": "pintu belakang",
		"patient_name": field(shown, "patient_name"), "medical_record_no": field(shown, "medical_record_no"),
	})
	if code, out := call("PUT", "/api/orders/1", string(body)); code != 200 {
		t.Fatalf("unchanged masked edit should succeed, got %d %v", code, out)
	}
	var saved models.Order
	db.First(&saved, order.ID)
	if saved.CustomerName != "Budi Santoso" || saved.CustomerPhone != "081234567890" ||
		saved.DropoffAddress != "Jl. Pemuda 12, Magelang" || *saved.PatientName != patient || *saved.MedicalRecordNo != mrn {
		t.Fatalf("masked values overwrote the real data: %+v", saved)
	}
	if saved.Notes == nil || *saved.Notes != "pintu belakang" {
		t.Fatal("the real change (notes) was not saved")
	}

	half := strings.Replace(string(body), `"B*** S******"`, `"B*** Santoso"`, 1)
	if code, _ := call("PUT", "/api/orders/1", half); code != fiber.StatusUnprocessableEntity {
		t.Fatalf("half-edited masked name must be rejected, got %d", code)
	}

	_, labels := call("POST", "/api/orders/labels", `{"ids":[1]}`)
	list, _ := labels["data"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["customer_name"] != "Budi Santoso" {
		t.Fatalf("labels must print the real data, got %v", labels)
	}
	if _, courier := call("GET", "/api/courier/orders/1", ""); field(courier, "customer_name") != "Budi Santoso" {
		t.Fatal("courier app must get the real data")
	}
}
