package handlers

import (
	"encoding/json"
	"log"
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
