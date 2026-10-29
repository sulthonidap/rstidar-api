package handlers

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
	"medsend-api/internal/storage"
)

// paymentMethodOf validates the payment method sent with a new order.
func paymentMethodOf(raw string) (enums.PaymentMethod, error) {
	m := enums.PaymentMethod(raw)
	if !m.Valid() {
		return "", fiber.NewError(fiber.StatusUnprocessableEntity, "pilih metode pembayaran (tunai atau non-tunai)")
	}
	return m, nil
}

// openTransaction creates the order's (unpaid) transaction when it's placed.
func openTransaction(tx *gorm.DB, order *models.Order, method enums.PaymentMethod) error {
	return tx.Create(&models.Transaction{
		OrderID:        order.ID,
		OrganizationID: order.OrganizationID,
		Amount:         order.Price,
		PaymentMethod:  &method,
		PaymentStatus:  enums.PaymentStatusUnpaid,
	}).Error
}

// syncTransactionAmount keeps a not-yet-paid transaction at the order's price
// (the price moves when the service or the destination changes).
func syncTransactionAmount(db *gorm.DB, order *models.Order) {
	db.Model(&models.Transaction{}).
		Where("order_id = ? AND payment_status = ?", order.ID, enums.PaymentStatusUnpaid).
		Update("amount", order.Price)
}

// isPaid: the patient has paid this order (its price is then what they paid).
func isPaid(db *gorm.DB, orderID uint) bool {
	var n int64
	db.Model(&models.Transaction{}).
		Where("order_id = ? AND payment_status = ?", orderID, enums.PaymentStatusPaid).
		Count(&n)
	return n > 0
}

// requirePaid: the medicine may only leave the RS once the order is paid.
// Orders from before payment methods (no method recorded) aren't blocked.
func requirePaid(db *gorm.DB, order *models.Order) error {
	var txn models.Transaction
	if err := db.Where("order_id = ?", order.ID).First(&txn).Error; err != nil || txn.PaymentMethod == nil {
		return nil
	}
	if txn.PaymentStatus == enums.PaymentStatusPaid {
		return nil
	}
	if *txn.PaymentMethod == enums.PaymentCash {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "order belum lunas: pasien bayar tunai di kasir RS dulu, lalu CCO tandai lunas")
	}
	return fiber.NewError(fiber.StatusUnprocessableEntity, "order belum lunas: CCO cek transfer/QRIS pasien dulu, lalu tandai lunas")
}

func withProofURL(files storage.Storage, t *models.Transaction) {
	if t != nil && t.Proof != nil && *t.Proof != "" {
		u := files.URL(*t.Proof)
		t.ProofURL = &u
	}
}

type paymentActionRequest struct {
	Action string  `json:"action"` // paid | refund
	Note   *string `json:"note"`
}

// Payment — POST /api/orders/:id/payment {action, note}:
//   - paid: the RS's CCO marks it paid after seeing the cash or the
//     transfer/QRIS come in (for now); Finance/Admin too;
//   - refund (a paid order): Finance/Admin.
func (h *OrderHandler) Payment(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)
	var order models.Order
	if err := h.DB.First(&order, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}
	var req paymentActionRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	var txn models.Transaction
	if err := h.DB.Where("order_id = ?", order.ID).First(&txn).Error; err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "order ini tidak punya tagihan")
	}

	role := user.RoleName()
	finance := role == enums.RoleAdmin || role == enums.RoleFinance
	approver := finance || role == enums.RoleCco
	note := req.Note
	if note != nil {
		n := strings.TrimSpace(*note)
		if len(n) > 255 {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "catatan maksimal 255 karakter")
		}
		note = &n
	}

	now := time.Now()
	switch req.Action {
	case "paid":
		if !approver {
			return fiber.NewError(fiber.StatusForbidden, "pembayaran di-ACC oleh CCO")
		}
		if txn.PaymentStatus == enums.PaymentStatusPaid {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "order ini sudah lunas")
		}
		if order.Status == enums.OrderStatusCancelled {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "order sudah dibatalkan, tidak perlu dibayar")
		}
		txn.PaymentStatus, txn.PaidAt, txn.Note = enums.PaymentStatusPaid, &now, note
	case "refund":
		if !finance {
			return fiber.NewError(fiber.StatusForbidden, "hanya Finance yang bisa me-refund")
		}
		if txn.PaymentStatus != enums.PaymentStatusPaid {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "hanya order lunas yang bisa di-refund")
		}
		txn.PaymentStatus, txn.Note = enums.PaymentStatusRefunded, note
	default:
		return fiber.NewError(fiber.StatusUnprocessableEntity, "aksi tidak dikenal")
	}
	txn.RecordedBy = &user.ID
	if err := h.DB.Save(&txn).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	withProofURL(h.Files, &txn)
	return c.JSON(fiber.Map{"data": txn})
}

// publicPayment: what the tracking page may show about the payment.
func publicPayment(t *models.Transaction) fiber.Map {
	if t == nil || t.PaymentMethod == nil {
		return nil
	}
	return fiber.Map{
		"method": t.PaymentMethod,
		"status": t.PaymentStatus,
		"amount": t.Amount,
	}
}

// rupiah formats an amount the Indonesian way: 35000 → "Rp 35.000".
func rupiah(v float64) string {
	s := strconv.FormatInt(int64(math.Round(v)), 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return "Rp " + s
}

// cancelPayment: what happens to a paid order's money when it's cancelled.
type cancelPayment string

const (
	cancelRefund cancelPayment = "refund" // money back to the patient
	cancelKeep   cancelPayment = "keep"   // no refund (e.g. the medicine was already prepared)
)

// checkCancelPayment: cancelling a paid order needs a choice — refund, or keep
// the money with a reason. Returns the paid transaction (nil when not paid).
func checkCancelPayment(db *gorm.DB, order *models.Order, payment cancelPayment, note *string) (*models.Transaction, error) {
	var txn models.Transaction
	if db.Where("order_id = ? AND payment_status = ?", order.ID, enums.PaymentStatusPaid).Limit(1).Find(&txn).RowsAffected == 0 {
		return nil, nil
	}
	// Settled at Rp0 because it was free (program ongkir gratis): no money to refund.
	if paidBecauseFree(txn) {
		return nil, nil
	}
	switch payment {
	case cancelRefund:
	case cancelKeep:
		if note == nil || strings.TrimSpace(*note) == "" {
			return nil, fiber.NewError(fiber.StatusUnprocessableEntity,
				"order sudah lunas dan tidak di-refund: isi alasannya")
		}
	default:
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity,
			fmt.Sprintf("order sudah lunas %s: pilih refund atau tidak", rupiah(txn.Amount)))
	}
	return &txn, nil
}

// refundOnCancel records the refund made while cancelling (by the CCO on the spot).
func refundOnCancel(db *gorm.DB, txn *models.Transaction, user *models.User, note *string) error {
	refundNote := "Refund karena order dibatalkan"
	if note != nil && strings.TrimSpace(*note) != "" {
		refundNote += ": " + strings.TrimSpace(*note)
	}
	if r := []rune(refundNote); len(r) > 255 {
		refundNote = string(r[:255])
	}
	txn.PaymentStatus, txn.Note, txn.RecordedBy = enums.PaymentStatusRefunded, &refundNote, &user.ID
	return db.Save(txn).Error
}
