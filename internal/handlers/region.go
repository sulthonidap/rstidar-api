package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/models"
)

// setServiceArea: kode wilayah yang dilayani (kab/kota, kecamatan, …), dipisah
// koma, diatur Admin di Pengaturan → Wilayah Layanan. Alamat tujuan order harus
// berada di salah satunya.
const setServiceArea = "service_area"

// loadServiceArea: the served region codes; nil (not set) = every region.
func loadServiceArea(db *gorm.DB) []string {
	var row models.AppSetting
	if db.Where("name = ?", setServiceArea).First(&row).Error != nil || row.Value == nil {
		return nil
	}
	var codes []string
	for _, c := range strings.Split(*row.Value, ",") {
		if c = strings.TrimSpace(c); c != "" {
			codes = append(codes, c)
		}
	}
	return codes
}

// inArea: code is one of the served regions or lies inside one.
func inArea(area []string, code string) bool {
	if len(area) == 0 {
		return true
	}
	for _, a := range area {
		if code == a || strings.HasPrefix(code, a+".") {
			return true
		}
	}
	return false
}

// regionOpen: code is served, or holds a served region (Jawa Tengah when only
// Kota Magelang is served), so the order form lists it.
func regionOpen(area []string, code string) bool {
	if inArea(area, code) {
		return true
	}
	for _, a := range area {
		if strings.HasPrefix(a, code+".") {
			return true
		}
	}
	return false
}

// regionLabel names a region down from its kab/kota, lowest first:
// "Kel. Tidar Utara, Kec. Magelang Selatan, Kota Magelang".
func regionLabel(db *gorm.DB, code string) (string, error) {
	parts := strings.Split(code, ".")
	codes := make([]string, 0, len(parts))
	for i := range parts {
		codes = append(codes, strings.Join(parts[:i+1], "."))
	}
	var rows []models.Region
	if err := db.Where("code IN ?", codes).Find(&rows).Error; err != nil {
		return "", err
	}
	byLevel := map[int]models.Region{}
	for _, r := range rows {
		byLevel[r.Level] = r
	}
	label := []string{}
	for level := len(parts); level >= 2; level-- {
		r, ok := byLevel[level]
		if !ok {
			continue
		}
		switch level {
		case 4:
			// Kemendagri: a village number starting with 1 is a kelurahan, with 2 a desa.
			if strings.HasPrefix(parts[3], "1") {
				label = append(label, "Kel. "+r.Name)
			} else {
				label = append(label, "Desa "+r.Name)
			}
		case 3:
			label = append(label, "Kec. "+r.Name)
		default:
			label = append(label, r.Name)
		}
	}
	if len(label) == 0 {
		if r, ok := byLevel[1]; ok {
			return r.Name, nil
		}
	}
	return strings.Join(label, ", "), nil
}

type RegionHandler struct {
	DB *gorm.DB
}

// Public — GET /api/public/regions?parent=33.71: the sub-regions the order
// forms list (only served ones); no parent = the provinces.
func (h *RegionHandler) Public(c *fiber.Ctx) error {
	return h.list(c, loadServiceArea(h.DB))
}

// Index — GET /api/regions?parent= (Admin): every sub-region, to set the service area.
func (h *RegionHandler) Index(c *fiber.Ctx) error {
	return h.list(c, nil)
}

func (h *RegionHandler) list(c *fiber.Ctx, area []string) error {
	query := h.DB.Order("name")
	if parent := strings.TrimSpace(c.Query("parent")); parent == "" {
		query = query.Where("level = 1")
	} else {
		query = query.Where("parent_code = ?", parent)
	}
	var rows []models.Region
	if err := query.Find(&rows).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	list := make([]models.Region, 0, len(rows))
	for _, r := range rows {
		if regionOpen(area, r.Code) {
			list = append(list, r)
		}
	}
	return c.JSON(fiber.Map{"data": list})
}

// serviceAreaRegion: one served region with its label, for the Admin page.
type serviceAreaRegion struct {
	Code  string `json:"code"`
	Level int    `json:"level"`
	Label string `json:"label"`
}

func (h *SettingsHandler) serviceArea() ([]serviceAreaRegion, error) {
	list := []serviceAreaRegion{}
	for _, code := range loadServiceArea(h.DB) {
		label, err := regionLabel(h.DB, code)
		if err != nil {
			return nil, err
		}
		list = append(list, serviceAreaRegion{Code: code, Level: len(strings.Split(code, ".")), Label: label})
	}
	return list, nil
}

// ServiceArea — GET /api/settings/service-area (Admin).
func (h *SettingsHandler) ServiceArea(c *fiber.Ctx) error {
	list, err := h.serviceArea()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": list})
}

// UpdateServiceArea — PUT /api/settings/service-area {codes} (Admin). A code
// inside another picked one adds nothing and is dropped.
func (h *SettingsHandler) UpdateServiceArea(c *fiber.Ctx) error {
	var req struct {
		Codes []string `json:"codes"`
	}
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	seen := map[string]bool{}
	codes := []string{}
	for _, code := range req.Codes {
		if code = strings.TrimSpace(code); code != "" && !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "pilih minimal satu wilayah layanan")
	}
	var found int64
	if err := h.DB.Model(&models.Region{}).Where("code IN ?", codes).Count(&found).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if int(found) != len(codes) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "ada kode wilayah yang tidak dikenal")
	}
	kept := []string{}
	for _, code := range codes {
		inside := false
		for _, other := range codes {
			if other != code && strings.HasPrefix(code, other+".") {
				inside = true
			}
		}
		if !inside {
			kept = append(kept, code)
		}
	}
	if err := h.set(h.DB, setServiceArea, strings.Join(kept, ",")); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return h.ServiceArea(c)
}

// regionInput: the destination kelurahan/desa on an order form. Required on a
// new order; on edit, empty (not sent) = keep.
type regionInput struct {
	DropoffRegionCode *string `json:"dropoff_region_code"`
}

// apply validates the region into o: a kelurahan/desa inside the service area.
// o's destination point is set already. checkPoint (a new or moved point): the
// point decides the kelurahan/desa (it's where the house really is), and a
// point outside the mapped service area is refused. An order keeps its region
// even after the Admin narrowed the area.
func (in regionInput) apply(db *gorm.DB, o *models.Order, required, checkPoint bool) error {
	code := trimOptional(in.DropoffRegionCode)
	if checkPoint && o.DropoffLat != nil && o.DropoffLng != nil {
		if found, ok := locateRegion(db, *o.DropoffLat, *o.DropoffLng); ok {
			code = &found
		} else if areaMapped(db, loadServiceArea(db)) {
			return fiber.NewError(fiber.StatusUnprocessableEntity,
				"titik lokasi tujuan di luar wilayah layanan, order tidak bisa dibuat")
		}
	}
	if code == nil {
		if required {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "pilih kelurahan/desa alamat tujuan")
		}
		return nil
	}
	if o.DropoffRegionCode != nil && *o.DropoffRegionCode == *code {
		return nil
	}
	var r models.Region
	if err := db.Where("code = ? AND level = 4", *code).First(&r).Error; err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "kelurahan/desa tujuan tidak ditemukan")
	}
	label, err := regionLabel(db, *code)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if !inArea(loadServiceArea(db), *code) {
		where := "alamat tujuan"
		if strings.TrimSpace(derefString(in.DropoffRegionCode)) != *code { // found from the point
			where = "titik lokasi tujuan"
		}
		return fiber.NewError(fiber.StatusUnprocessableEntity,
			where+" ("+label+") di luar wilayah layanan, order tidak bisa dibuat")
	}
	o.DropoffRegionCode, o.DropoffRegion = code, &label
	return nil
}
