package handlers

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
)

// setPharmacyPublic: "1" = form publik & kiosk juga menanyakan farmasi (wajib),
// diatur Admin di Pengaturan → Bangsal. Form petugas selalu menanyakannya.
const setPharmacyPublic = "pharmacy.public"

func pharmacyOnPublicForm(db *gorm.DB) bool {
	var row models.AppSetting
	return db.Where("name = ?", setPharmacyPublic).First(&row).Error == nil && row.Value != nil && *row.Value == "1"
}

type pharmacySettings struct {
	Public bool `json:"public"`
}

// PharmacySettings — GET /api/settings/pharmacy (Admin).
func (h *SettingsHandler) PharmacySettings(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"data": pharmacySettings{Public: pharmacyOnPublicForm(h.DB)}})
}

// UpdatePharmacySettings — PUT /api/settings/pharmacy (Admin).
func (h *SettingsHandler) UpdatePharmacySettings(c *fiber.Ctx) error {
	var req pharmacySettings
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	value := "0"
	if req.Public {
		value = "1"
	}
	if err := h.set(h.DB, setPharmacyPublic, value); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": req})
}

// PublicPharmacy — GET /api/public/pharmacy: whether the public form & kiosk ask
// for farmasi, and the active wards to pick from.
func (h *SettingsHandler) PublicPharmacy(c *fiber.Ctx) error {
	enabled := pharmacyOnPublicForm(h.DB)
	wards := []fiber.Map{}
	if enabled {
		var list []models.Ward
		h.DB.Where("is_active = ?", true).Order("name").Find(&list)
		for _, w := range list {
			wards = append(wards, fiber.Map{"id": w.ID, "name": w.Name, "is_active": true})
		}
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"enabled": enabled, "wards": wards}})
}

// pharmacyInput: farmasi asal resep di form order petugas — rawat jalan dengan
// no. antrian farmasi, atau rawat inap dengan bangsalnya. Wajib saat petugas
// membuat order; saat edit, care_type kosong (tidak dikirim) = tetap.
type pharmacyInput struct {
	CareType        *string `json:"care_type"`
	PharmacyQueueNo *string `json:"pharmacy_queue_no"`
	WardID          *uint   `json:"ward_id"`
}

// apply validates the input into o. required: the staff must fill it (create).
func (in pharmacyInput) apply(db *gorm.DB, o *models.Order, required bool) error {
	if in.CareType == nil || strings.TrimSpace(*in.CareType) == "" {
		if required {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "pilih farmasi: rawat jalan atau rawat inap")
		}
		return nil
	}
	care := enums.CareType(strings.TrimSpace(*in.CareType))
	if !care.Valid() {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "farmasi harus rawat jalan atau rawat inap")
	}
	switch care {
	case enums.CareRawatJalan:
		queue := trimOptional(in.PharmacyQueueNo)
		if queue == nil {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "no. antrian farmasi wajib diisi untuk rawat jalan")
		}
		if len(*queue) > 20 {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "no. antrian farmasi maksimal 20 karakter")
		}
		o.PharmacyQueueNo, o.WardID, o.Ward = queue, nil, nil
	case enums.CareRawatInap:
		if in.WardID == nil {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "pilih bangsal untuk rawat inap")
		}
		// An order keeps its (now inactive) ward; a new pick must be active.
		if o.WardID == nil || *o.WardID != *in.WardID {
			var w models.Ward
			if err := db.Where("id = ? AND is_active = ?", *in.WardID, true).First(&w).Error; err != nil {
				return fiber.NewError(fiber.StatusUnprocessableEntity, "bangsal tidak ditemukan atau nonaktif")
			}
		}
		o.WardID, o.PharmacyQueueNo, o.Ward = in.WardID, nil, nil
	}
	o.CareType = &care
	return nil
}

// pharmacyKey: what the farmasi fields say, to spot an edit ("farmasi" changed).
func pharmacyKey(o *models.Order) string {
	if o.CareType == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%d", *o.CareType, derefString(o.PharmacyQueueNo), derefUint(o.WardID))
}
