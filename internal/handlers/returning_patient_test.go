package handlers

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
)

func TestReturningPatient(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Organization{}, &models.Order{}); err != nil {
		t.Fatal(err)
	}
	org := models.Organization{Name: "RS Test", Slug: "rs-test", IsActive: true}
	db.Create(&org)
	rm := "RM-001"
	db.Create(&models.Order{
		Code: "MS-1", OrganizationID: &org.ID, CustomerName: "Anak", CustomerPhone: "+62 812-3456-7890",
		PickupAddress: "RS", DropoffAddress: "Jl. Lama", Status: enums.OrderStatusDone, Source: enums.OrderSourcePublic,
		MedicalRecordNo: &rm,
	})
	db.Create(&models.Order{
		Code: "MS-2", OrganizationID: &org.ID, CustomerName: "Anak", CustomerPhone: "081234567890",
		PickupAddress: "RS", DropoffAddress: "Jl. Baru", Status: enums.OrderStatusDone, Source: enums.OrderSourcePublic,
		MedicalRecordNo: &rm,
	})

	h := &PublicHandler{DB: db}
	app := fiber.New()
	app.Post("/rp", ReturningPatientLimit(), h.ReturningPatient)
	post := func(body string) (int, string) {
		req := httptest.NewRequest("POST", "/rp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	// The medical record number alone finds the latest order.
	if code, body := post(`{"query":"RM-001"}`); code != 200 || !strings.Contains(body, "Jl. Baru") {
		t.Fatalf("by RM: got %d %s", code, body)
	}
	// So does the phone alone, in any format (stored as "+62 812-3456-7890" and "0812…").
	for _, phone := range []string{"6281234567890", "0812-3456-7890", "+62 812 3456 7890"} {
		if code, body := post(`{"query":"` + phone + `"}`); code != 200 || !strings.Contains(body, "Jl. Baru") {
			t.Fatalf("by phone %s: got %d %s", phone, code, body)
		}
	}
	// Unknown: nothing is revealed.
	if code, body := post(`{"query":"089999999999"}`); code != 404 || strings.Contains(body, "Jl.") {
		t.Fatalf("unknown phone: got %d %s", code, body)
	}
	if code, _ := post(`{"query":"RM-999"}`); code != 404 {
		t.Fatalf("unknown RM: got %d", code)
	}
	if code, _ := post(`{"query":"  "}`); code != 422 {
		t.Fatalf("empty: got %d", code)
	}
	// 10 per minute: 7 requests so far, 3 more are fine, the 11th is refused.
	for i := 0; i < 3; i++ {
		if code, _ := post(`{"query":"RM-001"}`); code != 200 {
			t.Fatalf("request %d within the limit: got %d", 8+i, code)
		}
	}
	if code, _ := post(`{"query":"RM-001"}`); code != 429 {
		t.Fatalf("rate limit: got %d", code)
	}
}
