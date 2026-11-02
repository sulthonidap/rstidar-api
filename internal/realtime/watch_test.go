package realtime

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"medsend-api/internal/enums"
)

type watchedOrder struct {
	ID     uint
	Status string
}

func (watchedOrder) TableName() string { return "orders" }

type otherRow struct {
	ID   uint
	Name string
}

func (otherRow) TableName() string { return "services" }

func TestWatchOrdersSendsOneSilentChange(t *testing.T) {
	ChangedDelay = 20 * time.Millisecond
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&watchedOrder{}, &otherRow{}); err != nil {
		t.Fatal(err)
	}
	hub := NewHub()
	if err := hub.WatchOrders(db); err != nil {
		t.Fatal(err)
	}
	// Admin: none of the flow's steps are theirs, but the silent refresh is.
	events, stop := hub.Subscribe(1, enums.RoleAdmin)
	defer stop()

	next := func() (OrderEvent, bool) {
		select {
		case e := <-events:
			return e, true
		case <-time.After(200 * time.Millisecond):
			return OrderEvent{}, false
		}
	}

	// A burst of writes (create + update) arrives as a single event.
	o := watchedOrder{Status: "pending"}
	db.Create(&o)
	db.Model(&o).Update("status", "confirmed")
	if e, ok := next(); !ok || e.Type != "orders.changed" {
		t.Fatalf("want orders.changed, got %+v (received %v)", e, ok)
	}
	if e, ok := next(); ok {
		t.Fatalf("burst should coalesce into one event, got another: %+v", e)
	}

	// Other tables and no-op updates stay quiet.
	db.Create(&otherRow{Name: "Reguler"})
	db.Model(&watchedOrder{}).Where("id = ?", 999).Update("status", "x")
	if e, ok := next(); ok {
		t.Fatalf("unrelated writes must not notify, got %+v", e)
	}
}
