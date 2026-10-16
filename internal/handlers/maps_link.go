package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// Coordinates inside a Google Maps URL, most precise first: the place pin
// (!3d…!4d…), a query (?q= / query= / ll= / destination=), then the map view (@lat,lng).
var mapsCoordPatterns = []*regexp.Regexp{
	regexp.MustCompile(`!3d(-?\d{1,2}\.\d+)!4d(-?\d{1,3}\.\d+)`),
	regexp.MustCompile(`[?&](?:q|query|ll|destination|center)=(-?\d{1,2}\.\d+)(?:,|%2C)\s*(-?\d{1,3}\.\d+)`),
	regexp.MustCompile(`@(-?\d{1,2}\.\d+),(-?\d{1,3}\.\d+)`),
}

// googleHost: only Google's own map/short-link hosts may be fetched (no open proxy).
func googleHost(host string) bool {
	host = strings.ToLower(host)
	for _, suffix := range []string{"goo.gl", "google.com", "google.co.id", "app.goo.gl"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func coordsFromMapsURL(raw string) (float64, float64, bool) {
	if u, err := url.QueryUnescape(raw); err == nil {
		raw = u
	}
	for _, re := range mapsCoordPatterns {
		if m := re.FindStringSubmatch(raw); m != nil {
			lat, err1 := strconv.ParseFloat(m[1], 64)
			lng, err2 := strconv.ParseFloat(m[2], 64)
			if err1 == nil && err2 == nil {
				return lat, lng, true
			}
		}
	}
	return 0, 0, false
}

var mapsLinkClient = &http.Client{
	Timeout: 8 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("terlalu banyak redirect")
		}
		if req.URL.Scheme != "https" || !googleHost(req.URL.Hostname()) {
			return errors.New("redirect keluar dari Google Maps")
		}
		return nil
	},
}

// MapsLink — GET /api/public/maps-link?url=… → {lat, lng}
// Reads the coordinates of a Google Maps link, following short links
// (maps.app.goo.gl, as shared from the Maps app / WhatsApp), which the
// browser can't resolve itself.
func (h *PublicHandler) MapsLink(c *fiber.Ctx) error {
	raw := strings.TrimSpace(c.Query("url"))
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !googleHost(u.Hostname()) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "bukan link Google Maps")
	}

	final := raw
	if _, _, ok := coordsFromMapsURL(raw); !ok {
		req, _ := http.NewRequestWithContext(c.UserContext(), http.MethodGet, raw, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (MedSend)")
		res, err := mapsLinkClient.Do(req)
		if err != nil {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "link tidak bisa dibuka")
		}
		res.Body.Close()
		final = res.Request.URL.String()
		// Consent interstitial: the real map URL is in ?continue=.
		if next := res.Request.URL.Query().Get("continue"); next != "" {
			final = next
		}
	}

	lat, lng, ok := coordsFromMapsURL(final)
	if !ok || !validPoint(&lat, &lng) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "link tidak berisi titik lokasi di Indonesia")
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"lat": lat, "lng": lng}})
}
