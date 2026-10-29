package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

// seedRegions: a slice of Jawa Tengah, with Kab. + Kota Magelang served (as migration 26).
func seedRegions(db *gorm.DB) {
	rows := []struct {
		code, name string
	}{
		{"33", "Jawa Tengah"},
		{"33.08", "Kabupaten Magelang"},
		{"33.08.01", "Salaman"},
		{"33.08.01.2001", "Ngargoretno"},
		{"33.23", "Kabupaten Temanggung"},
		{"33.23.01", "Parakan"},
		{"33.23.01.1001", "Parakan Kauman"},
		{"33.71", "Kota Magelang"},
		{"33.71.01", "Magelang Selatan"},
		{"33.71.01.1010", "Tidar Utara"},
	}
	for _, r := range rows {
		parts := strings.Split(r.code, ".")
		region := models.Region{Code: r.code, Name: r.name, Level: len(parts)}
		if len(parts) > 1 {
			parent := strings.Join(parts[:len(parts)-1], ".")
			region.ParentCode = &parent
		}
		db.Create(&region)
	}
	db.Create(&models.AppSetting{Name: setServiceArea, Value: ptr("33.08,33.71")})
}

func TestServiceAreaHelpers(t *testing.T) {
	area := []string{"33.08", "33.71.01"}
	for code, want := range map[string]bool{
		"33.08": true, "33.08.01.2001": true, "33.71.01.1010": true,
		"33.71": false, "33.71.02.1003": false, "33.081": false, "33.23.01.1001": false,
	} {
		if inArea(area, code) != want {
			t.Errorf("inArea(%s) = %v", code, !want)
		}
	}
	if !inArea(nil, "33.23") {
		t.Error("no area set = every region")
	}
	// The form lists the parents of a served kecamatan.
	for code, want := range map[string]bool{"33": true, "33.71": true, "33.71.01": true, "33.71.02": false, "33.23": false} {
		if regionOpen(area, code) != want {
			t.Errorf("regionOpen(%s) = %v", code, !want)
		}
	}
}

func TestOrderServiceArea(t *testing.T) {
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
	cco := models.User{Name: "CCO", Email: "cco@test", Password: "x", IsActive: true}
	db.Create(&cco)

	h := NewOrderHandler(db, nil, nil, nil)
	pub := &PublicHandler{DB: db}
	regions := &RegionHandler{DB: db}
	settings := &SettingsHandler{DB: db}
	app := fiber.New()
	app.Get("/public/regions", regions.Public)
	app.Post("/public/orders", pub.StoreOrder)
	api := app.Group("/api", func(c *fiber.Ctx) error {
		c.Locals(middleware.UserContextKey, &cco)
		return c.Next()
	})
	api.Post("/orders", h.Store)
	api.Put("/orders/:id", h.Update)
	api.Put("/settings/service-area", settings.UpdateServiceArea)

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
	order := func(region any) map[string]any {
		body := map[string]any{
			"service_id": svc.ID, "customer_name": "Budi", "customer_phone": "081234567890",
			"dropoff_address": "Jl. Pemuda 12", "patient_name": "Budi", "medical_record_no": "RM1",
			"payment_method": "cash", "care_type": "rawat_jalan", "pharmacy_queue_no": "A-1",
		}
		if region != nil {
			body["dropoff_region_code"] = region
		}
		return body
	}

	// The form only lists served regions.
	if _, out := call("GET", "/public/regions?parent=33", nil); !strings.Contains(out, "Kota Magelang") ||
		!strings.Contains(out, "Kabupaten Magelang") || strings.Contains(out, "Temanggung") {
		t.Fatalf("listed regions: %s", out)
	}

	refused := []any{nil, "33.71.01", "33.71.01.9999", "33.23.01.1001"} // none, a kecamatan, unknown, outside the area
	for _, region := range refused {
		if code, _ := call("POST", "/api/orders", order(region)); code != fiber.StatusUnprocessableEntity {
			t.Fatalf("staff order to %v must be refused, got %d", region, code)
		}
		if code, _ := call("POST", "/public/orders", order(region)); code != fiber.StatusUnprocessableEntity {
			t.Fatalf("public order to %v must be refused, got %d", region, code)
		}
	}

	if code, out := call("POST", "/api/orders", order("33.71.01.1010")); code != 201 {
		t.Fatalf("staff order failed: %d %s", code, out)
	}
	var o models.Order
	db.First(&o)
	if derefString(o.DropoffRegion) != "Kel. Tidar Utara, Kec. Magelang Selatan, Kota Magelang" {
		t.Fatalf("label: %q", derefString(o.DropoffRegion))
	}
	if code, out := call("POST", "/public/orders", order("33.08.01.2001")); code != 201 {
		t.Fatalf("public order failed: %d %s", code, out)
	}
	var desa models.Order
	db.Last(&desa)
	if derefString(desa.DropoffRegion) != "Desa Ngargoretno, Kec. Salaman, Kabupaten Magelang" {
		t.Fatalf("label: %q", derefString(desa.DropoffRegion))
	}

	// Narrow the area to Kota Magelang: the Kab. Magelang order can still be edited
	// (keeps its region), but can't move to another region outside the area.
	if code, out := call("PUT", "/api/settings/service-area", map[string]any{"codes": []string{"33.71", "33.71.01"}}); code != 200 ||
		strings.Contains(out, `"33.71.01"`) {
		t.Fatalf("service area: %d %s", code, out)
	}
	if code, out := call("PUT", "/api/orders/2", order("33.08.01.2001")); code != 200 {
		t.Fatalf("keeping the region must work: %d %s", code, out)
	}
	if code, _ := call("PUT", "/api/orders/1", order("33.23.01.1001")); code != fiber.StatusUnprocessableEntity {
		t.Fatal("moving outside the area must be refused")
	}
	code, out := call("PUT", "/api/orders/2", order("33.71.01.1010"))
	if code != 200 || !strings.Contains(out, "wilayah tujuan") {
		t.Fatalf("moving inside the area: %d %s", code, out)
	}
	if code, _ := call("PUT", "/api/settings/service-area", map[string]any{"codes": []string{}}); code != fiber.StatusUnprocessableEntity {
		t.Fatal("an empty area must be refused")
	}
}

// square: a closed ring around (lat, lng), ±0.01°.
func square(lat, lng float64) string {
	ring := [][2]float64{{lat - 0.01, lng - 0.01}, {lat - 0.01, lng + 0.01}, {lat + 0.01, lng + 0.01}, {lat + 0.01, lng - 0.01}, {lat - 0.01, lng - 0.01}}
	raw, _ := json.Marshal([][][2]float64{ring})
	return string(raw)
}

func TestOrderPointRegion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Role{}, &models.Organization{}, &models.User{}, &models.Courier{},
		&models.Service{}, &models.ServiceTier{}, &models.Customer{}, &models.Order{}, &models.Ward{}, &models.Region{},
		&models.RegionBoundary{}, &models.OrderStatusHistory{}, &models.Transaction{}, &models.OrderDelivery{},
		&models.AppSetting{}); err != nil {
		t.Fatal(err)
	}
	rsLat, rsLng := -7.47, 110.21
	db.Create(&models.Organization{Name: "RSUD Tidar", Lat: &rsLat, Lng: &rsLng})
	seedRegions(db)
	for _, b := range []models.RegionBoundary{
		{Code: "33.71.01.1010", Lat: -7.49, Lng: 110.23, Path: square(-7.49, 110.23)}, // Tidar Utara
		{Code: "33.08.01.2001", Lat: -7.59, Lng: 110.11, Path: square(-7.59, 110.11)}, // Ngargoretno
		{Code: "33.23.01.1001", Lat: -7.29, Lng: 110.09, Path: square(-7.29, 110.09)}, // Parakan (not served)
	} {
		db.Create(&b)
	}
	svc := models.Service{Name: "Reguler", PriceBase: 12000, IsActive: true}
	db.Create(&svc)
	cco := models.User{Name: "CCO", Email: "cco@test", Password: "x", IsActive: true}
	db.Create(&cco)

	h := NewOrderHandler(db, nil, nil, nil)
	pub := &PublicHandler{DB: db}
	app := fiber.New()
	app.Post("/public/quote", pub.Quote)
	app.Get("/public/regions/area", (&RegionHandler{DB: db}).Area)
	api := app.Group("/api", func(c *fiber.Ctx) error {
		c.Locals(middleware.UserContextKey, &cco)
		return c.Next()
	})
	api.Post("/orders", h.Store)
	api.Put("/orders/:id/coordinates", h.Coordinates)

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
	order := func(region string, lat, lng any) map[string]any {
		return map[string]any{
			"service_id": svc.ID, "customer_name": "Budi", "customer_phone": "081234567890",
			"dropoff_address": "Jl. Pemuda 12", "patient_name": "Budi", "medical_record_no": "RM1",
			"payment_method": "cash", "care_type": "rawat_jalan", "pharmacy_queue_no": "A-1",
			"dropoff_region_code": region, "dropoff_lat": lat, "dropoff_lng": lng,
		}
	}
	last := func() models.Order {
		var o models.Order
		db.Order("id DESC").First(&o)
		return o
	}

	// The point wins over the dropdown: it's where the house is.
	if code, out := call("POST", "/api/orders", order("33.08.01.2001", -7.491, 110.231)); code != 201 {
		t.Fatalf("order with point: %d %s", code, out)
	}
	if o := last(); derefString(o.DropoffRegionCode) != "33.71.01.1010" || derefString(o.DistanceSource) != distanceEstimate {
		t.Fatalf("region from the point: %+v", o)
	}

	// A point in an unserved kelurahan, or outside every outline, is refused.
	if code, out := call("POST", "/api/orders", order("33.71.01.1010", -7.291, 110.091)); code != 422 || !strings.Contains(out, "titik lokasi") {
		t.Fatalf("point in Parakan: %d %s", code, out)
	}
	if code, _ := call("POST", "/api/orders", order("33.71.01.1010", -7.0, 110.0)); code != 422 {
		t.Fatal("point outside every outline must be refused")
	}

	// No point: measured to the kelurahan's centre.
	if code, out := call("POST", "/api/orders", order("33.08.01.2001", nil, nil)); code != 201 {
		t.Fatalf("order without point: %d %s", code, out)
	}
	if o := last(); derefString(o.DistanceSource) != distanceRegion || o.DistanceKm == nil || *o.DistanceKm < 10 {
		t.Fatalf("distance from the centre: %+v", o)
	}

	// Placing the point later moves the order to that kelurahan.
	id := last().ID
	if code, out := call("PUT", fmt.Sprintf("/api/orders/%d/coordinates", id), map[string]any{"dropoff_lat": -7.489, "dropoff_lng": 110.229}); code != 200 {
		t.Fatalf("coordinates: %d %s", code, out)
	}
	if o := last(); derefString(o.DropoffRegionCode) != "33.71.01.1010" || derefString(o.DropoffRegion) == "" {
		t.Fatalf("region after moving the point: %+v", o)
	}
	if code, _ := call("PUT", fmt.Sprintf("/api/orders/%d/coordinates", id), map[string]any{"dropoff_lat": -7.29, "dropoff_lng": 110.09}); code != 422 {
		t.Fatal("moving the point out of the area must be refused")
	}

	// The form map gets only the served outlines (not Parakan) and their bounds.
	if _, out := call("GET", "/public/regions/area", nil); !strings.Contains(out, `"bounds":[[-7.6,110.1],[-7.48,`) ||
		strings.Count(out, "],[") < 8 || strings.Contains(out, "-7.3") {
		t.Fatalf("area: %s", out)
	}

	// The quote tells the form where the point is.
	if _, out := call("POST", "/public/quote", map[string]any{"dropoff_lat": -7.49, "dropoff_lng": 110.23}); !strings.Contains(out, `"code":"33.71.01.1010"`) ||
		!strings.Contains(out, `"outside_area":false`) {
		t.Fatalf("quote inside: %s", out)
	}
	if _, out := call("POST", "/public/quote", map[string]any{"dropoff_lat": -7.29, "dropoff_lng": 110.09}); !strings.Contains(out, `"outside_area":true`) {
		t.Fatalf("quote outside: %s", out)
	}
	if _, out := call("POST", "/public/quote", map[string]any{"dropoff_region_code": "33.08.01.2001"}); !strings.Contains(out, `"distance_source":"region"`) {
		t.Fatalf("quote from the centre: %s", out)
	}
}
