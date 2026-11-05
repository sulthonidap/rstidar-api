package handlers

import (
	"log"

	"github.com/gofiber/fiber/v2"

	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

// PrivateData — GET /api/orders/:id/private: nomor HP, No. RM, dan alamat asli,
// dipanggil saat petugas menekan ikon mata di panel (lolos dari MaskPatientData).
// Tiap pembukaan dicatat di log.
func (h *OrderHandler) PrivateData(c *fiber.Ctx) error {
	var order models.Order
	if err := h.DB.First(&order, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}
	logPrivateView(c, "order "+order.Code)
	return c.JSON(fiber.Map{"data": fiber.Map{
		"customer_phone":    order.CustomerPhone,
		"medical_record_no": order.MedicalRecordNo,
		"dropoff_address":   order.DropoffAddress,
	}})
}

// PrivateData — GET /api/customers/:id/private: lihat OrderHandler.PrivateData.
func (h *CustomerHandler) PrivateData(c *fiber.Ctx) error {
	customer, err := h.find(c)
	if err != nil {
		return err
	}
	logPrivateView(c, "customer "+customer.Name)
	return c.JSON(fiber.Map{"data": fiber.Map{"phone": customer.Phone, "address": customer.Address}})
}

func logPrivateView(c *fiber.Ctx, what string) {
	user := middleware.CurrentUser(c)
	log.Printf("data pasien dibuka: %s oleh %s (id %d)", what, user.Email, user.ID)
}
