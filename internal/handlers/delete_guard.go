package handlers

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/models"
)

// usage is one kind of record that still points at the row being deleted.
type usage struct {
	label string
	model any
	where string
}

// refuseIfUsed returns a 409 listing what still references the row (e.g. "12 order,
// 3 user"). The FKs are ON DELETE SET NULL, so deleting a used layanan would
// silently strip it from order history and reports — deactivate instead.
func refuseIfUsed(db *gorm.DB, what string, id uint, usages []usage) error {
	var parts []string
	for _, u := range usages {
		var n int64
		if err := db.Model(u.model).Where(u.where, id).Count(&n).Error; err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, u.label))
		}
	}
	if len(parts) > 0 {
		return fiber.NewError(fiber.StatusConflict, fmt.Sprintf(
			"%s ini masih dipakai %s. Nonaktifkan saja lewat Edit supaya riwayat tetap utuh.",
			what, strings.Join(parts, ", ")))
	}
	return nil
}

// Destroy — DELETE /api/services/:id (hanya kalau belum dipakai order).
func (h *ServiceHandler) Destroy(c *fiber.Ctx) error {
	var svc models.Service
	if err := h.DB.First(&svc, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "layanan tidak ditemukan")
	}
	if err := refuseIfUsed(h.DB, "Layanan", svc.ID, []usage{
		{"order", &models.Order{}, "service_id = ?"},
	}); err != nil {
		return err
	}
	if err := h.DB.Delete(&svc).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}
