package services_test

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
	"medsend-api/internal/services"
)

// setupTestDB — in-memory SQLite lewat GORM AutoMigrate, dipakai buat
// nguji OrderService tanpa perlu MySQL nyala (mirip phpunit.xml pakai
// SQLite in-memory di backend Laravel). DSN dikasih nama unik per test
// (mode=memory + cache=shared) biar tiap test dapet database sendiri,
// bukan berbagi satu in-memory DB antar test dalam proses yang sama.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	err = db.AutoMigrate(
		&models.Organization{}, &models.Role{}, &models.User{}, &models.Service{},
		&models.Courier{}, &models.Order{}, &models.OrderStatusHistory{}, &models.Transaction{},
	)
	if err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	return db
}

func seedRole(t *testing.T, db *gorm.DB, name string) uint {
	t.Helper()
	role := models.Role{Name: name}
	if err := db.Create(&role).Error; err != nil {
		t.Fatalf("seed role %s: %v", name, err)
	}
	return role.ID
}

func TestFullLifecycleCreatesTransactionOnDone(t *testing.T) {
	db := setupTestDB(t)
	kurirRoleID := seedRole(t, db, string(enums.RoleKurir))

	org := models.Organization{Name: "RS Test", Slug: "rs-test", IsActive: true}
	db.Create(&org)

	svc := models.Service{Name: "Instan", PriceBase: 50000, IsActive: true}
	db.Create(&svc)

	courierUser := models.User{Name: "Kurir", Email: "k1@test.local", Password: "x", RoleID: &kurirRoleID, IsActive: true}
	db.Create(&courierUser)
	courier := models.Courier{UserID: courierUser.ID, IsActive: true}
	db.Create(&courier)
	db.Preload("User").First(&courier, courier.ID)

	order := models.Order{
		Code: "MS-TEST-0001", OrganizationID: &org.ID, CustomerName: "Budi",
		CustomerPhone: "0812", PickupAddress: "A", DropoffAddress: "B",
		ServiceID: &svc.ID, Status: enums.OrderStatusConfirmed, Source: enums.OrderSourceCco,
		Price: svc.PriceBase,
	}
	db.Create(&order)

	svcOrder := services.NewOrderService(db)

	if err := svcOrder.AssignCourier(&order, &courier, nil); err != nil {
		t.Fatalf("assign courier: %v", err)
	}
	if order.Status != enums.OrderStatusConfirmed {
		t.Fatalf("assigning a courier must not change the status, got %s", order.Status)
	}
	if order.AssignedAt == nil {
		t.Fatal("expected assigned_at to be set")
	}

	// Farmasi → CCO pickup + packing → courier takes it → done.
	for _, to := range []enums.OrderStatus{
		enums.OrderStatusReadyForPickup, enums.OrderStatusPacked, enums.OrderStatusOnDelivery,
	} {
		if err := svcOrder.Transition(&order, to, nil, nil); err != nil {
			t.Fatalf("transition %s: %v", to, err)
		}
	}
	if order.ReadyAt == nil || order.CollectedAt == nil || order.PackedAt == nil || order.PickedUpAt == nil {
		t.Fatal("expected every step's timestamp to be set")
	}
	if err := svcOrder.Transition(&order, enums.OrderStatusDone, nil, nil); err != nil {
		t.Fatalf("transition done: %v", err)
	}

	if order.Status != enums.OrderStatusDone {
		t.Fatalf("expected status done, got %s", order.Status)
	}
	if order.DeliveredAt == nil {
		t.Fatal("expected delivered_at to be set")
	}

	var txn models.Transaction
	if err := db.Where("order_id = ?", order.ID).First(&txn).Error; err != nil {
		t.Fatalf("expected transaction auto-created, got error: %v", err)
	}
	if txn.Amount != 50000 {
		t.Fatalf("expected transaction amount 50000, got %v", txn.Amount)
	}
	if txn.PaymentStatus != enums.PaymentStatusUnpaid {
		t.Fatalf("expected payment_status unpaid, got %s", txn.PaymentStatus)
	}

	var historyCount int64
	db.Model(&models.OrderStatusHistory{}).Where("order_id = ?", order.ID).Count(&historyCount)
	if historyCount != 4 { // ready, packed (collected + packed), on delivery, done
		t.Fatalf("expected 4 status history rows, got %d", historyCount)
	}
}

func TestIllegalTransitionIsRejected(t *testing.T) {
	db := setupTestDB(t)

	order := models.Order{
		Code: "MS-TEST-0002", CustomerName: "X", CustomerPhone: "1",
		PickupAddress: "A", DropoffAddress: "B", Status: enums.OrderStatusPending, Source: enums.OrderSourcePublic,
	}
	db.Create(&order)

	err := services.NewOrderService(db).Transition(&order, enums.OrderStatusDone, nil, nil)
	if err == nil {
		t.Fatal("expected error transitioning pending -> done directly, got nil")
	}
}

func TestActiveCourierCanBeAssigned(t *testing.T) {
	db := setupTestDB(t)
	kurirRoleID := seedRole(t, db, string(enums.RoleKurir))

	org := models.Organization{Name: "RS X", Slug: "rs-x", IsActive: true}
	db.Create(&org)

	courierUser := models.User{Name: "Kurir", Email: "kurir@test.local", Password: "x", RoleID: &kurirRoleID, IsActive: true}
	db.Create(&courierUser)
	courier := models.Courier{UserID: courierUser.ID, IsActive: true}
	db.Create(&courier)
	db.Preload("User").First(&courier, courier.ID)

	order := models.Order{
		Code: "MS-TEST-0004", OrganizationID: &org.ID, CustomerName: "X", CustomerPhone: "1",
		PickupAddress: "A", DropoffAddress: "B", Status: enums.OrderStatusPacked, Source: enums.OrderSourcePublic,
	}
	db.Create(&order)

	if err := services.NewOrderService(db).AssignCourier(&order, &courier, nil); err != nil {
		t.Fatalf("expected active courier assignable, got error: %v", err)
	}
	if order.CourierID == nil || *order.CourierID != courier.ID {
		t.Fatal("expected courier_id to be set")
	}
}

func TestCourierCanOnlyBeAssignedBetweenConfirmationAndHandover(t *testing.T) {
	db := setupTestDB(t)
	kurirRoleID := seedRole(t, db, string(enums.RoleKurir))

	courierUser := models.User{Name: "Kurir 2", Email: "fl2@test.local", Password: "x", RoleID: &kurirRoleID, IsActive: true}
	db.Create(&courierUser)
	courier := models.Courier{UserID: courierUser.ID, IsActive: true}
	db.Create(&courier)

	for i, status := range []enums.OrderStatus{enums.OrderStatusPending, enums.OrderStatusOnDelivery, enums.OrderStatusDone} {
		order := models.Order{
			Code: fmt.Sprintf("MS-TEST-10%d", i), CustomerName: "X", CustomerPhone: "1",
			PickupAddress: "A", DropoffAddress: "B", Status: status, Source: enums.OrderSourcePublic,
		}
		db.Create(&order)
		if err := services.NewOrderService(db).AssignCourier(&order, &courier, nil); err == nil {
			t.Errorf("expected assigning a courier to a %s order to fail", status)
		}
	}
}

func TestStaffRolesPerStep(t *testing.T) {
	cases := []struct {
		from, to enums.OrderStatus
		role     enums.UserRole
		want     bool
	}{
		{enums.OrderStatusPending, enums.OrderStatusConfirmed, enums.RoleCco, true},
		{enums.OrderStatusPending, enums.OrderStatusConfirmed, enums.RoleFarmasi, false},
		{enums.OrderStatusConfirmed, enums.OrderStatusReadyForPickup, enums.RoleFarmasi, true},
		{enums.OrderStatusConfirmed, enums.OrderStatusReadyForPickup, enums.RoleCco, false},
		{enums.OrderStatusReadyForPickup, enums.OrderStatusPacked, enums.RoleCco, true},
		{enums.OrderStatusReadyForPickup, enums.OrderStatusPacked, enums.RoleOps, false},
		{enums.OrderStatusReadyForPickup, enums.OrderStatusPacked, enums.RoleFarmasi, false},
		{enums.OrderStatusPacked, enums.OrderStatusOnDelivery, enums.RoleCco, false},   // courier only
		{enums.OrderStatusPacked, enums.OrderStatusOnDelivery, enums.RoleAdmin, false}, // courier only
		{enums.OrderStatusPacked, enums.OrderStatusCancelled, enums.RoleCco, true},
		{enums.OrderStatusOnDelivery, enums.OrderStatusCancelled, enums.RoleCco, false},
		{enums.OrderStatusOnDelivery, enums.OrderStatusCancelled, enums.RoleAdmin, true},
		{enums.OrderStatusConfirmed, enums.OrderStatusReadyForPickup, enums.RoleAdmin, true},
	}
	for _, c := range cases {
		if got := c.from.StaffCanMove(c.role, c.to); got != c.want {
			t.Errorf("%s: %s -> %s = %v, want %v", c.role, c.from, c.to, got, c.want)
		}
	}
}
