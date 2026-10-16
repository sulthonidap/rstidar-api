package handlers

import (
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/models"
)

type CourierHandler struct {
	DB *gorm.DB
}

// Index — GET /api/couriers (Admin, Ops).
func (h *CourierHandler) Index(c *fiber.Ctx) error {
	var couriers []models.Courier
	if err := h.DB.Preload("User").Order("id").Find(&couriers).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": couriers})
}

type courierRequest struct {
	UserID      uint    `json:"user_id" validate:"required"`
	VehicleType *string `json:"vehicle_type"`
	Phone       *string `json:"phone"`
	IsActive    *bool   `json:"is_active"`
}

func (h *CourierHandler) Store(c *fiber.Ctx) error {
	var req courierRequest
	if err := c.BodyParser(&req); err != nil || req.UserID == 0 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "user_id wajib diisi")
	}
	// One courier record per account: creating a Kurir user already makes one, and
	// a second one would split the account (the app reads one, orders go to the other).
	var existing int64
	if err := h.DB.Model(&models.Courier{}).Where("user_id = ?", req.UserID).Count(&existing).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if existing > 0 {
		return fiber.NewError(fiber.StatusConflict, "akun ini sudah terdaftar sebagai kurir, ubah data kurirnya saja")
	}

	courier := models.Courier{
		UserID:      req.UserID,
		VehicleType: req.VehicleType,
		Phone:       req.Phone,
		IsActive:    true,
	}
	if req.IsActive != nil {
		courier.IsActive = *req.IsActive
	}

	if err := h.DB.Create(&courier).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	h.DB.Preload("User").First(&courier, courier.ID)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": courier})
}

func (h *CourierHandler) Update(c *fiber.Ctx) error {
	var courier models.Courier
	if err := h.DB.First(&courier, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "kurir tidak ditemukan")
	}

	var req courierRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}

	courier.VehicleType = req.VehicleType
	courier.Phone = req.Phone
	if req.IsActive != nil {
		courier.IsActive = *req.IsActive
	}

	if err := h.DB.Save(&courier).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	h.DB.Preload("User").First(&courier, courier.ID)
	return c.JSON(fiber.Map{"data": courier})
}
