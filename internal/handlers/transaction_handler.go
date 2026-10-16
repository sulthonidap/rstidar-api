package handlers

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
	"medsend-api/internal/storage"
)

type TransactionHandler struct {
	DB    *gorm.DB
	Files storage.Storage // payment receipts
}

// filtered — transaksi yang boleh dilihat user, difilter ?search= dan
// ?from=YYYY-MM-DD&to=YYYY-MM-DD (tanggal dibuat, inklusif). Dipakai Index & Export
// supaya layar dan file Excel selalu berisi data yang sama.
func (h *TransactionHandler) filtered(c *fiber.Ctx) (*gorm.DB, error) {
	query := h.DB.Model(&models.Transaction{})

	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := likePattern(search)
		query = query.Where(
			"order_id IN (SELECT id FROM orders WHERE code LIKE ? OR customer_name LIKE ?)",
			like, like,
		)
	}
	query, err := filterCreatedDate(c, query)
	if err != nil {
		return nil, err
	}
	return query.Session(&gorm.Session{}), nil
}

// Index — GET /api/transactions?search=&from=&to=&page=&per_page=
// Response: {data, meta, summary} — total dari semua halaman: total_amount = uang
// yang benar-benar masuk (lunas); refund dan tagihan belum bayar dipisah (tagihan
// order yang dibatalkan sebelum dibayar tidak dihitung lagi).
func (h *TransactionHandler) Index(c *fiber.Ctx) error {
	page := parsePage(c)
	query, err := h.filtered(c)
	if err != nil {
		return err
	}

	var summary struct {
		Total          int64
		TotalAmount    float64
		RefundedAmount float64
		UnpaidAmount   float64
	}
	err = query.Select(`COUNT(*) AS total,
		COALESCE(SUM(CASE WHEN payment_status = @paid THEN amount ELSE 0 END), 0) AS total_amount,
		COALESCE(SUM(CASE WHEN payment_status = @refunded THEN amount ELSE 0 END), 0) AS refunded_amount,
		COALESCE(SUM(CASE WHEN payment_status = @unpaid
			AND order_id NOT IN (SELECT id FROM orders WHERE status = @cancelled) THEN amount ELSE 0 END), 0) AS unpaid_amount`,
		map[string]any{
			"paid": enums.PaymentStatusPaid, "refunded": enums.PaymentStatusRefunded,
			"unpaid": enums.PaymentStatusUnpaid, "cancelled": enums.OrderStatusCancelled,
		}).Scan(&summary).Error
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	var list []models.Transaction
	err = query.Preload("Order").Preload("Organization").Preload("RecordedByUser").
		Order("created_at DESC, id DESC").
		Scopes(page.scope).
		Find(&list).Error
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	for i := range list {
		withProofURL(h.Files, &list[i])
	}
	return c.JSON(fiber.Map{
		"data": list,
		"meta": page.meta(summary.Total),
		"summary": fiber.Map{
			"total_amount":    summary.TotalAmount,
			"refunded_amount": summary.RefundedAmount,
			"unpaid_amount":   summary.UnpaidAmount,
		},
	})
}

type updatePaymentRequest struct {
	PaymentStatus enums.PaymentStatus `json:"payment_status" validate:"required"`
	Amount        *float64            `json:"amount"`
}

// UpdatePayment — PATCH /api/transactions/:id. Nggak ada Store: transaksi
// dibuat otomatis oleh OrderService.Transition saat order Done.
func (h *TransactionHandler) UpdatePayment(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var txn models.Transaction
	if err := h.DB.First(&txn, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "transaksi tidak ditemukan")
	}

	var req updatePaymentRequest
	if err := c.BodyParser(&req); err != nil || !req.PaymentStatus.Valid() {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "payment_status tidak valid")
	}

	txn.PaymentStatus = req.PaymentStatus
	if req.Amount != nil {
		txn.Amount = *req.Amount
	}
	txn.RecordedBy = &user.ID

	if req.PaymentStatus == enums.PaymentStatusPaid {
		now := time.Now()
		txn.PaidAt = &now
	} else {
		txn.PaidAt = nil
	}

	if err := h.DB.Save(&txn).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	h.DB.Preload("Order").Preload("Organization").Preload("RecordedByUser").First(&txn, txn.ID)
	return c.JSON(fiber.Map{"data": txn})
}
