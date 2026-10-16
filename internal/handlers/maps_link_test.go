package handlers

import "testing"

func TestCoordsFromMapsURL(t *testing.T) {
	cases := []struct {
		url      string
		lat, lng float64
		ok       bool
	}{
		{"https://www.google.com/maps/place/Monas/@-6.1753924,106.8249641,17z/data=!3m1!4b1!4m6!3m5!1s0x0:0x0!8m2!3d-6.1753924!4d106.8271528", -6.1753924, 106.8271528, true},
		{"https://maps.google.com/?q=-6.2601,106.7812", -6.2601, 106.7812, true},
		{"https://www.google.com/maps/search/?api=1&query=-6.2253%2C106.8331", -6.2253, 106.8331, true},
		{"https://www.google.com/maps/@-6.2,106.8,15z", -6.2, 106.8, true},
		{"https://maps.app.goo.gl/AbCdEf", 0, 0, false},
	}
	for _, c := range cases {
		lat, lng, ok := coordsFromMapsURL(c.url)
		if ok != c.ok || (ok && (lat != c.lat || lng != c.lng)) {
			t.Errorf("%s → %v,%v ok=%v; want %v,%v ok=%v", c.url, lat, lng, ok, c.lat, c.lng, c.ok)
		}
	}
}

func TestGoogleHostOnly(t *testing.T) {
	for host, want := range map[string]bool{
		"maps.app.goo.gl": true, "www.google.com": true, "maps.google.co.id": true,
		"evil.com": false, "google.com.evil.com": false, "notgoogle.com": false,
	} {
		if got := googleHost(host); got != want {
			t.Errorf("googleHost(%q) = %v, want %v", host, got, want)
		}
	}
}
