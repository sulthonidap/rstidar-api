package handlers

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/models"
)

type RoleHandler struct {
	DB *gorm.DB
}

func (h *RoleHandler) Index(c *fiber.Ctx) error {
	var roles []models.Role
	if err := h.DB.Order("id").Find(&roles).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": roles})
}
