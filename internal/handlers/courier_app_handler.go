package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
	"medsend-api/internal/services"
	"medsend-api/internal/storage"
)

// CourierAppHandler — endpoint untuk aplikasi kurir (/api/courier/*). Semua
// request sudah lewat RequireAuth + role Kurir + RequireCourier, jadi kurirnya
// ada di context dan kurir cuma bisa lihat/ubah order yang di-assign ke dia.
type CourierAppHandler struct {
	DB       *gorm.DB
	OrderSvc *services.OrderService
	Store    storage.Storage // proof-of-delivery photos
}

func NewCourierAppHandler(db *gorm.DB, store storage.Storage) *CourierAppHandler {
	return &CourierAppHandler{DB: db, OrderSvc: services.NewOrderService(db), Store: store}
}

// courierMoves — status yang boleh diubah kurir sendiri (sisanya urusan staff):
// terima paket dari CCO (scan label) lalu selesai antar.
var courierMoves = map[enums.OrderStatus]enums.OrderStatus{
	enums.OrderStatusPacked:     enums.OrderStatusOnDelivery,
	enums.OrderStatusOnDelivery: enums.OrderStatusDone,
}

func (h *CourierAppHandler) myOrders(c *fiber.Ctx) *gorm.DB {
	courier := middleware.CurrentCourier(c)
	return h.DB.Model(&models.Order{}).Where("courier_id = ?", courier.ID)
}

// Me — GET /api/courier/me
func (h *CourierAppHandler) Me(c *fiber.Ctx) error {
	courier := middleware.CurrentCourier(c)
	courier.User = middleware.CurrentUser(c)
	return c.JSON(fiber.Map{"data": courier})
}

// finishedAt: when a history order ended (delivered or cancelled).
const finishedAt = "COALESCE(delivered_at, cancelled_at, updated_at)"

// Orders — GET /api/courier/orders?scope=active|history&page=&per_page=
// active: yang ditugaskan ke kurir dan belum selesai (termasuk yang paketnya
// masih disiapkan), terlama di-assign dulu.
// history: Done & Cancelled, terbaru dulu; ?from=&to=YYYY-MM-DD menyaring
// tanggal order selesai/dibatalkan.
func (h *CourierAppHandler) Orders(c *fiber.Ctx) error {
	page := parsePage(c)
	query := h.myOrders(c)
	order := "assigned_at ASC, id ASC"
	if c.Query("scope") == "history" {
		query = query.Where("status IN ?", []enums.OrderStatus{enums.OrderStatusDone, enums.OrderStatusCancelled})
		var err error
		if query, err = filterDate(c, query, finishedAt); err != nil {
			return err
		}
		order = finishedAt + " DESC, id DESC"
	} else {
		query = query.Where("status IN ?", enums.ActiveCourierStatuses)
	}
	query = query.Session(&gorm.Session{})

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	orders := []models.Order{}
	if err := query.Preload("Organization").Preload("Service").Order(order).Scopes(page.scope).Find(&orders).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": orders, "meta": page.meta(total)})
}

func (h *CourierAppHandler) findMine(c *fiber.Ctx) (*models.Order, error) {
	var order models.Order
	err := h.myOrders(c).Preload("Organization").Preload("Service").Preload("Delivery").
		Preload("StatusHistory", func(db *gorm.DB) *gorm.DB { return db.Order("changed_at DESC, id DESC") }).
		First(&order, "id = ?", c.Params("id")).Error
	if err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}
	return &order, nil
}

// Show — GET /api/courier/orders/:id
func (h *CourierAppHandler) Show(c *fiber.Ctx) error {
	order, err := h.findMine(c)
	if err != nil {
		return err
	}
	withPhotoURLs(h.Store, order.Delivery)
	return c.JSON(fiber.Map{"data": order})
}

type courierStatusRequest struct {
	Status enums.OrderStatus `json:"status"`
	Note   *string           `json:"note"`
	// Scanned from the shipping label when taking the package, so the courier
	// can't accept an order without holding the right parcel.
	Code string `json:"code"`
}

// UpdateStatus — POST /api/courier/orders/:id/status {status, note, code}
// Kurir cuma boleh Siap kirim → Dalam pengiriman (wajib kode label yang cocok).
// Selesai lewat Deliver (wajib bukti terima).
func (h *CourierAppHandler) UpdateStatus(c *fiber.Ctx) error {
	order, err := h.findMine(c)
	if err != nil {
		return err
	}

	var req courierStatusRequest
	if err := c.BodyParser(&req); err != nil || !req.Status.Valid() {
		return fiber.NewError(fiber.StatusBadRequest, "status tidak valid")
	}
	if next, ok := courierMoves[order.Status]; !ok || next != req.Status {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "kurir tidak bisa mengubah status "+order.Status.Label()+" ke "+req.Status.Label())
	}

	if req.Status == enums.OrderStatusDone {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "selesaikan lewat form bukti terima (foto + nama penerima)")
	}
	if req.Status == enums.OrderStatusOnDelivery && !strings.EqualFold(strings.TrimSpace(req.Code), order.Code) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "kode label tidak cocok dengan order ini, pastikan paketnya benar")
	}

	user := middleware.CurrentUser(c)
	if err := h.OrderSvc.Transition(order, req.Status, &user.ID, req.Note); err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}

	updated, err := h.findMine(c)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": updated})
}
