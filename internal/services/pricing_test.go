package services_test

import (
	"testing"

	"medsend-api/internal/models"
	"medsend-api/internal/services"
)

func ptr(v float64) *float64 { return &v }

func TestQuote(t *testing.T) {
	svc := models.Service{
		PriceBase:  15000,
		PricePerKm: ptr(3000),
		Tiers: []models.ServiceTier{ // deliberately unsorted
			{UpToKm: 7, Price: 25000},
			{UpToKm: 3, Price: 15000},
			{UpToKm: 15, Price: 40000},
		},
	}
	cases := []struct {
		name string
		km   *float64
		want float64
	}{
		{"no distance → base", nil, 15000},
		{"1 km → tier 1", ptr(1), 15000},
		{"exactly 3 km → tier 1", ptr(3), 15000},
		{"3.1 km → tier 2", ptr(3.1), 25000},
		{"15 km → tier 3", ptr(15), 40000},
		{"15.2 km → +1 started km", ptr(15.2), 43000},
		{"18 km → +3 km", ptr(18), 49000},
	}
	for _, c := range cases {
		if got := services.Quote(svc, c.km); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}

	flat := models.Service{PriceBase: 20000}
	if got := services.Quote(flat, ptr(50)); got != 20000 {
		t.Errorf("service without tiers: got %v, want flat 20000", got)
	}
	noPerKm := models.Service{PriceBase: 1, Tiers: []models.ServiceTier{{UpToKm: 5, Price: 10000}}}
	if got := services.Quote(noPerKm, ptr(9)); got != 10000 {
		t.Errorf("beyond last tier without per-km: got %v, want 10000", got)
	}
}

func TestEstimateKm(t *testing.T) {
	// Monas → Blok M is ~8.3 km straight; ×1.3 ≈ 10.8 km.
	km := services.EstimateKm(-6.1754, 106.8272, -6.2442, 106.7982)
	if km < 10.5 || km > 11.1 {
		t.Errorf("estimate = %.1f km, want ≈10.8", km)
	}
}

func TestInRange(t *testing.T) {
	cases := []struct {
		name  string
		maxKm *float64
		km    *float64
		want  bool
	}{
		{"no limit", nil, ptr(500), true},
		{"unknown distance", ptr(30), nil, true},
		{"under the limit", ptr(30), ptr(12.5), true},
		{"exactly the limit", ptr(30), ptr(30), true},
		{"over the limit", ptr(30), ptr(30.1), false},
	}
	for _, c := range cases {
		if got := services.InRange(c.maxKm, c.km); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
