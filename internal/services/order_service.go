package services

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
)

// OrderService adalah satu-satunya pintu buat ubah status order & assign
// kurir: transisi status yang sah, assign kurir, dan auto-create transaksi.
type OrderService struct {
	DB *gorm.DB
}

func NewOrderService(db *gorm.DB) *OrderService {
	return &OrderService{DB: db}
}

// Transition pindahin order ke status baru: validasi transisi, catat
// order_status_history, isi kolom timestamp, dan bikin Transaction otomatis
// saat status jadi "done".
func (s *OrderService) Transition(order *models.Order, to enums.OrderStatus, actorID *uint, note *string) error {
	from := order.Status

	if from == to {
		return fmt.Errorf("order sudah berstatus %s", to.Label())
	}

	if !from.CanTransitionTo(to) {
		return fmt.Errorf("transisi %s → %s tidak diizinkan", from.Label(), to.Label())
	}

	if to == enums.OrderStatusOnDelivery && order.CourierID == nil {
		return errors.New("order belum punya kurir, minta Ops menugaskan kurir dulu")
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		order.Status = to
		now := time.Now()

		stamps := map[string]**time.Time{
			"confirmed_at": &order.ConfirmedAt,
			"ready_at":     &order.ReadyAt,
			"packed_at":    &order.PackedAt,
			"picked_up_at": &order.PickedUpAt,
			"delivered_at": &order.DeliveredAt,
		}
		if at, ok := stamps[to.TimestampColumn()]; ok && *at == nil {
			*at = &now
		}
		// "Siap kirim" means the CCO collected the medicine and packed it in one
		// step; collected_at is still filled for reports.
		if to == enums.OrderStatusPacked && order.CollectedAt == nil {
			order.CollectedAt = &now
		}

		if to == enums.OrderStatusCancelled && order.CancelledAt == nil {
			order.CancelledAt = &now
		}

		if err := tx.Save(order).Error; err != nil {
			return err
		}

		history := models.OrderStatusHistory{
			OrderID:    order.ID,
			FromStatus: &from,
			ToStatus:   to,
			ChangedBy:  actorID,
			Note:       note,
			ChangedAt:  now,
		}
		if err := tx.Create(&history).Error; err != nil {
			return err
		}

		if to == enums.OrderStatusDone {
			if err := s.recordTransaction(tx, order, actorID); err != nil {
				return err
			}
		}

		return nil
	})
}

// AssignCourier assign (atau ganti) kurir order. Bukan perubahan status —
// boleh dari Confirmed sampai Siap kirim, selama paketnya belum diterima kurir.
func (s *OrderService) AssignCourier(order *models.Order, courier *models.Courier, actorID *uint) error {
	if !courier.IsActive {
		return errors.New("kurir tidak aktif")
	}

	if !order.Status.IsAssignable() {
		if order.Status == enums.OrderStatusPending {
			return errors.New("order belum dikonfirmasi CCO")
		}
		return fmt.Errorf("order sudah %s, kurir tidak bisa diganti", order.Status.Label())
	}

	now := time.Now()
	order.CourierID = &courier.ID
	order.AssignedAt = &now
	return s.DB.Save(order).Error
}

func (s *OrderService) recordTransaction(tx *gorm.DB, order *models.Order, actorID *uint) error {
	var count int64
	if err := tx.Model(&models.Transaction{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	txn := models.Transaction{
		OrderID:        order.ID,
		OrganizationID: order.OrganizationID,
		Amount:         order.Price,
		PaymentStatus:  enums.PaymentStatusUnpaid,
		RecordedBy:     actorID,
	}
	return tx.Create(&txn).Error
}

// GenerateOrderCode bikin nomor order MS-YYYYMMDD-0001, mirip
// Order::generateCode() di Laravel.
func GenerateOrderCode(db *gorm.DB) (string, error) {
	today := time.Now().Format("20060102")

	var count int64
	startOfDay := time.Now().Truncate(24 * time.Hour)
	if err := db.Model(&models.Order{}).
		Where("created_at >= ?", startOfDay).
		Count(&count).Error; err != nil {
		return "", err
	}

	return fmt.Sprintf("MS-%s-%04d", today, count+1), nil
}
