package handlers

import (
	"encoding/json"
	"log"
	"math"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/models"
)

// boundary: a kelurahan/desa outline (migration 27), for finding which one a
// map point lies in.
type boundary struct {
	code                           string
	lat, lng                       float64 // centre
	rings                          [][][2]float64
	minLat, maxLat, minLng, maxLng float64
}

// contains: even-odd over every ring, so holes and islands come out right.
func (b *boundary) contains(lat, lng float64) bool {
	if lat < b.minLat || lat > b.maxLat || lng < b.minLng || lng > b.maxLng {
		return false
	}
	in := false
	for _, ring := range b.rings {
		for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
			yi, xi, yj, xj := ring[i][0], ring[i][1], ring[j][0], ring[j][1]
			if (yi > lat) != (yj > lat) && lng < (xj-xi)*(lat-yi)/(yj-yi)+xi {
				in = !in
			}
		}
	}
	return in
}

// The outlines never change at runtime (they come with a migration): read once
// per database.
var boundaryCache struct {
	sync.Mutex
	db   *gorm.DB
	list []boundary
}

func loadBoundaries(db *gorm.DB) []boundary {
	boundaryCache.Lock()
	defer boundaryCache.Unlock()
	if boundaryCache.db == db && boundaryCache.list != nil {
		return boundaryCache.list
	}
	var rows []models.RegionBoundary
	if err := db.Find(&rows).Error; err != nil {
		log.Printf("region boundaries: %v", err)
		return nil
	}
	list := make([]boundary, 0, len(rows))
	for _, r := range rows {
		b := boundary{code: r.Code, lat: r.Lat, lng: r.Lng, minLat: 90, maxLat: -90, minLng: 180, maxLng: -180}
		if err := json.Unmarshal([]byte(r.Path), &b.rings); err != nil {
			log.Printf("region boundary %s: %v", r.Code, err)
			continue
		}
		for _, ring := range b.rings {
			for _, p := range ring {
				b.minLat, b.maxLat = min(b.minLat, p[0]), max(b.maxLat, p[0])
				b.minLng, b.maxLng = min(b.minLng, p[1]), max(b.maxLng, p[1])
			}
		}
		list = append(list, b)
	}
	boundaryCache.db, boundaryCache.list = db, list
	return list
}

// locateRegion: the kelurahan/desa a map point lies in, if its outline is known.
func locateRegion(db *gorm.DB, lat, lng float64) (string, bool) {
	for _, b := range loadBoundaries(db) {
		if b.contains(lat, lng) {
			return b.code, true
		}
	}
	return "", false
}

// regionCentre: a kelurahan/desa's centre point, to measure the distance when
// the order has no map point.
func regionCentre(db *gorm.DB, code string) (float64, float64, bool) {
	for _, b := range loadBoundaries(db) {
		if b.code == code {
			return b.lat, b.lng, true
		}
	}
	return 0, 0, false
}

// areaMapped: every served region has outlines, so a map point outside all of
// them is outside the service area. (A region the Admin added later without
// outlines falls back to the picked kelurahan/desa.)
func areaMapped(db *gorm.DB, area []string) bool {
	list := loadBoundaries(db)
	if len(area) == 0 || len(list) == 0 {
		return false
	}
	for _, a := range area {
		found := false
		for _, b := range list {
			if b.code == a || strings.HasPrefix(b.code, a+".") {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Map — GET /api/regions/map (Admin): every kelurahan/desa outline with whether
// it's served, for the map on Pengaturan → Wilayah Layanan.
func (h *RegionHandler) Map(c *fiber.Ctx) error {
	list := loadBoundaries(h.DB)
	codes := make([]string, 0, len(list)*2)
	for _, b := range list {
		codes = append(codes, b.code, b.code[:min(len(b.code), 8)])
	}
	var regions []models.Region
	if err := h.DB.Select("code, name").Where("code IN ?", codes).Find(&regions).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	names := make(map[string]string, len(regions))
	for _, r := range regions {
		names[r.Code] = r.Name
	}
	area := loadServiceArea(h.DB)
	out := make([]fiber.Map, 0, len(list))
	for _, b := range list {
		out = append(out, fiber.Map{
			"code":     b.code,
			"name":     names[b.code],
			"district": names[b.code[:min(len(b.code), 8)]],
			"in_area":  inArea(area, b.code),
			"rings":    b.rings,
		})
	}
	return c.JSON(fiber.Map{"data": out})
}

// simplifyRing: Douglas–Peucker (tolerance in degrees), coordinates rounded to
// 5 decimals (~1 m) — for drawing, not for locating points.
func simplifyRing(ring [][2]float64, tol float64) [][2]float64 {
	if len(ring) <= 4 {
		return ring
	}
	keep := make([]bool, len(ring))
	keep[0], keep[len(ring)-1] = true, true
	type span struct{ a, b int }
	stack := []span{{0, len(ring) - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		ay, ax, by, bx := ring[s.a][0], ring[s.a][1], ring[s.b][0], ring[s.b][1]
		dx, dy := bx-ax, by-ay
		length := math.Hypot(dx, dy)
		far, idx := 0.0, -1
		for i := s.a + 1; i < s.b; i++ {
			var d float64
			if length == 0 { // a closed ring's ends meet
				d = math.Hypot(ring[i][1]-ax, ring[i][0]-ay)
			} else {
				d = math.Abs(dy*ring[i][1]-dx*ring[i][0]+bx*ay-by*ax) / length
			}
			if d > far {
				far, idx = d, i
			}
		}
		if far > tol && idx > 0 {
			keep[idx] = true
			stack = append(stack, span{s.a, idx}, span{idx, s.b})
		}
	}
	out := make([][2]float64, 0, len(ring)/4)
	for i, p := range ring {
		if keep[i] {
			out = append(out, [2]float64{math.Round(p[0]*1e5) / 1e5, math.Round(p[1]*1e5) / 1e5})
		}
	}
	if len(out) < 4 {
		return ring
	}
	return out
}

// Area — GET /api/public/regions/area: the served kelurahan/desa outlines
// (simplified, for drawing) and their bounds, so the order forms' map shades
// what's outside and stays around the area. data null = no outlines known.
func (h *RegionHandler) Area(c *fiber.Ctx) error {
	area := loadServiceArea(h.DB)
	rings := [][][2]float64{}
	minLat, maxLat, minLng, maxLng := 90.0, -90.0, 180.0, -180.0
	for _, b := range loadBoundaries(h.DB) {
		if !inArea(area, b.code) {
			continue
		}
		for _, ring := range b.rings {
			rings = append(rings, simplifyRing(ring, 0.00015))
		}
		minLat, maxLat = min(minLat, b.minLat), max(maxLat, b.maxLat)
		minLng, maxLng = min(minLng, b.minLng), max(maxLng, b.maxLng)
	}
	if len(rings) == 0 {
		return c.JSON(fiber.Map{"data": nil})
	}
	return c.JSON(fiber.Map{"data": fiber.Map{
		"bounds": [2][2]float64{{minLat, minLng}, {maxLat, maxLng}},
		"rings":  rings,
	}})
}
