// cmd/seed mengisi data awal: role, admin, 2 RS lengkap dengan staff +
// kurir + order contoh. Porting dari database/seeders/DatabaseSeeder.php.
package main

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"medsend-api/internal/config"
	"medsend-api/internal/database"
	"medsend-api/internal/enums"
	"medsend-api/internal/models"
	"medsend-api/internal/services"
)

func main() {
	cfg := config.Load()
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}

	hash := func(pw string) string {
		b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			log.Fatal(err)
		}
		return string(b)
	}
	password := hash("password")

	// --- roles ---
	roleDefs := []struct{ name, label string }{
		{string(enums.RoleAdmin), "Admin"},
		{string(enums.RoleOps), "Ops (Penugasan)"},
		{string(enums.RoleCco), "CCO (Kelola Order)"},
		{string(enums.RoleKurir), "Kurir"},
		{string(enums.RoleFinance), "Finance"},
		{string(enums.RoleFarmasi), "Farmasi RS"},
	}
	roleIDs := map[string]uint{}
	for _, r := range roleDefs {
		role := models.Role{Name: r.name, Label: &r.label}
		db.Where(models.Role{Name: r.name}).Assign(models.Role{Label: &r.label}).FirstOrCreate(&role)
		roleIDs[r.name] = role.ID
	}

	// --- admin ---
	admin := models.User{
		Name: "Admin", Email: "admin@medsend.test", Password: password,
		RoleID: ptr(roleIDs[string(enums.RoleAdmin)]), IsActive: true,
	}
	db.Where(models.User{Email: admin.Email}).Assign(admin).FirstOrCreate(&admin)

	// --- layanan ---
	serviceDefs := []models.Service{
		{Name: "Reguler", Description: ptrStr("Pengiriman standar, estimasi harian"), PriceBase: 15000, IsActive: true},
		{Name: "Same Day", Description: ptrStr("Sampai di hari yang sama"), PriceBase: 30000, IsActive: true},
		{Name: "Instan", Description: ptrStr("Langsung berangkat, 1-2 jam"), PriceBase: 50000, IsActive: true},
	}
	var svcs []models.Service
	for _, s := range serviceDefs {
		svc := s
		db.Where(models.Service{Name: s.Name}).Assign(s).FirstOrCreate(&svc)
		svcs = append(svcs, svc)
	}

	// Contoh tarif per jarak (Reguler): s/d 3 km, 7 km, 15 km, lalu +Rp 3.000/km.
	reg := svcs[0]
	db.Model(&reg).Update("price_per_km", 3000)
	db.Where("service_id = ?", reg.ID).Delete(&models.ServiceTier{})
	for _, t := range [][2]float64{{3, 15000}, {7, 25000}, {15, 40000}} {
		db.Create(&models.ServiceTier{ServiceID: reg.ID, UpToKm: t[0], Price: t[1]})
	}

	// --- kurir ---
	budi := makeCourier(db, "Kurir Budi", "budi@kurir.test", password, roleIDs)
	makeCourier(db, "Kurir Sari", "sari@kurir.test", password, roleIDs)

	// --- RS mitra (satu-satunya; di deploy asli diisi admin lewat Setup Profil RS) ---
	{
		org := models.Organization{
			Name:     "RS Harapan Bunda",
			Slug:     "rs-harapan-bunda",
			Address:  ptrStr("Jl. Contoh No. 10, Jakarta"),
			Phone:    ptrStr("021-5550001"),
			Lat:      ptr(-6.1805), // contoh titik di Jakarta
			Lng:      ptr(106.8284),
			IsActive: true,
		}
		db.Where(models.Organization{Name: org.Name}).Assign(org).FirstOrCreate(&org)

		var cco models.User
		for _, r := range []enums.UserRole{enums.RoleOps, enums.RoleCco, enums.RoleFinance, enums.RoleFarmasi} {
			email := fmt.Sprintf("%s@medsend.test", r)
			u := models.User{
				Name: strings.ToUpper(string(r[:1])) + string(r[1:]), Email: email, Password: password,
				RoleID: ptr(roleIDs[string(r)]), IsActive: true,
			}
			db.Where(models.User{Email: email}).Assign(u).FirstOrCreate(&u)
			if r == enums.RoleCco {
				cco = u
			}
		}

		// Pelanggan tersimpan (buat autocomplete "customer lama" di form order internal).
		for j, name := range []string{"Budi Santoso", "Siti Aminah", "Andi Wijaya"} {
			phone := fmt.Sprintf("0813%08d", j+1)
			cust := models.Customer{
				OrganizationID: org.ID, Name: name, Phone: phone,
				Address: fmt.Sprintf("Jl. Melati No. %d, Jakarta Selatan", (j+1)*7), CreatedBy: &cco.ID,
			}
			db.Where(models.Customer{OrganizationID: org.ID, Phone: phone}).Assign(cust).FirstOrCreate(&cust)
		}

		makeOrder(db, org.ID, svcs[0].ID, nil, enums.OrderStatusPending, enums.OrderSourcePublic, nil)
		makeOrder(db, org.ID, svcs[1].ID, nil, enums.OrderStatusConfirmed, enums.OrderSourceCco, &cco.ID)
		makeOrder(db, org.ID, svcs[0].ID, &budi.ID, enums.OrderStatusReadyForPickup, enums.OrderSourcePublic, nil)
		makeOrder(db, org.ID, svcs[1].ID, &budi.ID, enums.OrderStatusPacked, enums.OrderSourceCco, &cco.ID)
		makeOrder(db, org.ID, svcs[2].ID, &budi.ID, enums.OrderStatusOnDelivery, enums.OrderSourcePublic, nil)
	}

	log.Println("seed selesai. Login: admin@medsend.test / password")
}

func makeCourier(db *gorm.DB, name, email, password string, roleIDs map[string]uint) models.Courier {
	user := models.User{
		Name: name, Email: email, Password: password,
		RoleID: ptr(roleIDs[string(enums.RoleKurir)]), IsActive: true,
	}
	db.Where(models.User{Email: email}).Assign(user).FirstOrCreate(&user)

	phone := fmt.Sprintf("0812%08d", rand.Intn(99999999))
	courier := models.Courier{
		UserID:      user.ID,
		VehicleType: ptrStr("Motor"), Phone: &phone, IsActive: true,
	}
	db.Where(models.Courier{UserID: user.ID}).Assign(courier).FirstOrCreate(&courier)
	return courier
}

func makeOrder(db *gorm.DB, orgID, serviceID uint, courierID *uint, status enums.OrderStatus, source enums.OrderSource, createdBy *uint) {
	var svc models.Service
	db.First(&svc, serviceID)

	code, err := services.GenerateOrderCode(db)
	if err != nil {
		log.Fatal(err)
	}

	n := rand.Intn(999) + 1
	now := time.Now()
	order := models.Order{
		Code: code, OrganizationID: &orgID,
		CustomerName:  fmt.Sprintf("Pasien %d", n),
		CustomerPhone: fmt.Sprintf("08%010d", n),
		PickupAddress: "Farmasi Lt. 1", DropoffAddress: fmt.Sprintf("Jl. Pengantaran No. %d, Jakarta", n),
		ServiceID: &serviceID, CourierID: courierID,
		Status: status, Source: source, CreatedBy: createdBy,
		Price: svc.PriceBase,
	}

	// Fill the timestamp of every step up to the order's status.
	steps := []struct {
		status enums.OrderStatus
		at     **time.Time
	}{
		{enums.OrderStatusConfirmed, &order.ConfirmedAt},
		{enums.OrderStatusReadyForPickup, &order.ReadyAt},
		{enums.OrderStatusPacked, &order.PackedAt},
		{enums.OrderStatusOnDelivery, &order.PickedUpAt},
		{enums.OrderStatusDone, &order.DeliveredAt},
	}
	reached := 0 // pending (and anything not listed): no step timestamps
	for i, st := range steps {
		if st.status == status {
			reached = i + 1
		}
	}
	for i := 0; i < reached; i++ {
		t := now.Add(time.Duration(i-reached) * 30 * time.Minute)
		*steps[i].at = &t
	}
	order.CollectedAt = order.PackedAt // collected + packed is one step
	if courierID != nil && order.ConfirmedAt != nil {
		order.AssignedAt = order.ConfirmedAt
	}

	db.Create(&order)
}

func ptr[T any](v T) *T       { return &v }
func ptrStr(s string) *string { return &s }
