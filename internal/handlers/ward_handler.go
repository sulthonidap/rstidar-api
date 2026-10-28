package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/models"
)

// WardHandler: master bangsal rawat inap. Semua staff melihat (dropdown di form
// order), Admin yang menambah/mengubah.
type WardHandler struct {
	DB *gorm.DB
}

// Index — GET /api/wards?active=1
func (h *WardHandler) Index(c *fiber.Ctx) error {
	q := h.DB.Order("name")
	if c.Query("active") == "1" {
		q = q.Where("is_active = ?", true)
	}
	var list []models.Ward
	if err := q.Find(&list).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": list})
}

type wardRequest struct {
	Name     string `json:"name"`
	IsActive *bool  `json:"is_active"`
}

// fill validates the request into w; the name is unique (case-insensitive).
func (h *WardHandler) fill(c *fiber.Ctx, w *models.Ward) error {
	var req wardRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "nama bangsal wajib diisi")
	}
	if len(name) > 100 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "nama bangsal maksimal 100 karakter")
	}
	var n int64
	h.DB.Model(&models.Ward{}).Where("name = ? AND id <> ?", name, w.ID).Count(&n)
	if n > 0 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "bangsal "+name+" sudah ada")
	}
	w.Name = name
	if req.IsActive != nil {
		w.IsActive = *req.IsActive
	}
	return nil
}

// Store — POST /api/wards
func (h *WardHandler) Store(c *fiber.Ctx) error {
	w := models.Ward{IsActive: true}
	if err := h.fill(c, &w); err != nil {
		return err
	}
	if err := h.DB.Create(&w).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": w})
}

// Update — PUT /api/wards/:id
func (h *WardHandler) Update(c *fiber.Ctx) error {
	var w models.Ward
	if err := h.DB.First(&w, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "bangsal tidak ditemukan")
	}
	if err := h.fill(c, &w); err != nil {
		return err
	}
	if err := h.DB.Save(&w).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": w})
}

// Destroy — DELETE /api/wards/:id (hanya kalau belum dipakai order).
func (h *WardHandler) Destroy(c *fiber.Ctx) error {
	var w models.Ward
	if err := h.DB.First(&w, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "bangsal tidak ditemukan")
	}
	if err := refuseIfUsed(h.DB, "Bangsal", w.ID, []usage{
		{"order", &models.Order{}, "ward_id = ?"},
	}); err != nil {
		return err
	}
	if err := h.DB.Delete(&w).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
