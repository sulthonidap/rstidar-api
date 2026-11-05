package handlers

import (
	"fmt"
	"log"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

// Destroy — DELETE /api/orders/:id (Admin), body {"confirm":"<kode order>"}: hapus
// permanen satu order (mis. data testing) beserta riwayat, transaksi, bukti kirim,
// perhentian rute, dan fotonya. Sengaja tanpa tombol di panel: dipanggil dari
// terminal/Postman. :id boleh id atau kode order. Tanpa confirm yang cocok tidak
// ada yang terhapus, dan responsnya menyebut kode order itu, jadi id yang salah
// ketik ketahuan dulu. Data customer tidak ikut terhapus (DELETE /api/customers/:id).
func (h *OrderHandler) Destroy(c *fiber.Ctx) error {
	var order models.Order
	query := h.DB
	if id, err := strconv.ParseUint(c.Params("id"), 10, 64); err == nil {
		query = query.Where("id = ?", id)
	} else {
		query = query.Where("code = ?", c.Params("id"))
	}
	if err := query.First(&order).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}

	var body struct {
		Confirm string `json:"confirm"`
	}
	_ = c.BodyParser(&body)
	if body.Confirm != order.Code {
		return fiber.NewError(fiber.StatusUnprocessableEntity,
			fmt.Sprintf("kirim {\"confirm\": %q} untuk menghapus order ini permanen", order.Code))
	}

	// Their keys live in the rows we're about to delete.
	keys := h.orderFileKeys(order.ID)
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		for _, child := range []any{
			&models.CourierRouteStop{}, &models.OrderDelivery{}, &models.OrderStatusHistory{}, &models.Transaction{},
		} {
			if err := tx.Where("order_id = ?", order.ID).Delete(child).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&order).Error
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "gagal menghapus order: "+err.Error())
	}

	// Files are best-effort: a leftover photo is harmless, the data is already gone.
	failed := 0
	if h.Files != nil {
		for _, k := range keys {
			if err := h.Files.Delete(c.UserContext(), k); err != nil {
				failed++
			}
		}
	}
	user := middleware.CurrentUser(c)
	log.Printf("order dihapus permanen: %s (id %d, status %s) oleh %s (id %d)", order.Code, order.ID, order.Status, user.Email, user.ID)

	return c.JSON(fiber.Map{
		"message":       "Order " + order.Code + " dihapus permanen",
		"code":          order.Code,
		"files_deleted": len(keys) - failed,
		"files_failed":  failed,
	})
}

// orderFileKeys lists an order's uploaded files: POD photos/thumbnails and the payment receipt.
func (h *OrderHandler) orderFileKeys(orderID uint) []string {
	var keys []string
	var delivery models.OrderDelivery
	if h.DB.Select("photos", "thumbs").Where("order_id = ?", orderID).Limit(1).Find(&delivery).RowsAffected > 0 {
		keys = append(keys, delivery.Photos...)
		keys = append(keys, delivery.Thumbs...)
	}
	var transactions []models.Transaction
	h.DB.Select("proof").Where("order_id = ? AND proof IS NOT NULL", orderID).Find(&transactions)
	for _, t := range transactions {
		if t.Proof != nil && *t.Proof != "" {
			keys = append(keys, *t.Proof)
		}
	}
	return keys
}
