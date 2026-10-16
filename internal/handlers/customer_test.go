package handlers

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"medsend-api/internal/models"
)

func TestNormalizePhone(t *testing.T) {
	for in, want := range map[string]string{
		"089677271000":      "089677271000",
		"+62 896-7727-1000": "089677271000",
		"6289677271000":     "089677271000",
		"(0896) 7727 1000":  "089677271000",
	} {
		if got := normalizePhone(in); got != want {
			t.Errorf("normalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

// One phone shared by a family must not make each new name overwrite the last.
func TestRememberCustomerPhoneAndName(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Customer{}); err != nil {
		t.Fatal(err)
	}
	remember := func(name, phone, address string) uint {
		t.Helper()
		id, err := rememberCustomer(db, 1, nil, customerContact{Name: name, Phone: phone, Address: address})
		if err != nil || id == nil {
			t.Fatalf("rememberCustomer(%s): id=%v err=%v", name, id, err)
		}
		return *id
	}

	bapak := remember("Bapak", "089677271000", "Jl. A")
	ibu := remember("Ibu", "+62 896-7727-1000", "Jl. A")
	if bapak == ibu {
		t.Fatal("same phone, different name must be a different customer")
	}
	again := remember(" Bapak ", "6289677271000", "Jl. B")
	if again != bapak {
		t.Fatal("same phone and name (any phone format) must be the same customer")
	}

	var count int64
	db.Model(&models.Customer{}).Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 customers, got %d", count)
	}
	var c models.Customer
	db.First(&c, bapak)
	if c.Address != "Jl. B" || c.Phone != "089677271000" {
		t.Fatalf("expected the latest address and a normalized phone, got %q %q", c.Address, c.Phone)
	}
}
