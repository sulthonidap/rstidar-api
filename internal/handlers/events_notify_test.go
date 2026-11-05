package handlers

import (
	"testing"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
	"medsend-api/internal/realtime"
)

// Admin hears every step of the flow, but not the ones they did themselves.
func TestPublishOrderEventTellsAdmin(t *testing.T) {
	hub := realtime.NewHub()
	admin, stop := hub.Subscribe(1, enums.RoleAdmin)
	defer stop()
	farmasi, stopFarmasi := hub.Subscribe(2, enums.RoleFarmasi)
	defer stopFarmasi()
	got := func(ch <-chan realtime.OrderEvent) bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	}

	publishOrderEvent(hub, "order.created", &models.Order{Code: "MS-1", Status: enums.OrderStatusPending}, nil)
	if !got(admin) {
		t.Fatal("a new order must reach Admin")
	}
	if got(farmasi) {
		t.Fatal("a new order isn't Farmasi's step")
	}

	self := uint(1)
	publishOrderEvent(hub, "order.status", &models.Order{Code: "MS-1", Status: enums.OrderStatusConfirmed}, &self)
	if got(admin) {
		t.Fatal("Admin must not be pinged about their own action")
	}
	if !got(farmasi) {
		t.Fatal("a confirmed order must still reach Farmasi")
	}

	// Steps nobody acts on stay silent.
	publishOrderEvent(hub, "order.status", &models.Order{Code: "MS-1", Status: enums.OrderStatusDone}, nil)
	if got(admin) {
		t.Fatal("a finished order must not notify")
	}
}
