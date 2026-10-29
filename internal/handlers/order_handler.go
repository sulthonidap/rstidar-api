package handlers

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
	"medsend-api/internal/privacy"
	"medsend-api/internal/realtime"
	"medsend-api/internal/routing"
	"medsend-api/internal/services"
	"medsend-api/internal/storage"
)

type OrderHandler struct {
	DB     *gorm.DB
	Orders *services.OrderService
	Hub    *realtime.Hub
	Files  storage.Storage // proof-of-delivery photos
	Router routing.Router  // road distances for pricing (nil = straight-line estimate)
}

func NewOrderHandler(db *gorm.DB, hub *realtime.Hub, store storage.Storage, router routing.Router) *OrderHandler {
	return &OrderHandler{DB: db, Orders: services.NewOrderService(db), Hub: hub, Files: store, Router: router}
}

func orderPreload(db *gorm.DB) *gorm.DB {
	return db.
		Preload("Organization").
		Preload("Service").
		Preload("Ward").
		Preload("Courier.User").
		Preload("Creator")
}

// Index — GET /api/orders?status=&courier_id=&search=&from=&to=&schedule=&page=&per_page=
// Response: {data, meta, summary: {by_status}} — summary dihitung dengan filter
// yang sama KECUALI status, untuk kartu ringkasan per status di atas tabel.
func (h *OrderHandler) Index(c *fiber.Ctx) error {
	page := parsePage(c)

	query := h.DB.Model(&models.Order{})

	if courierID := c.Query("courier_id"); courierID != "" {
		query = query.Where("courier_id = ?", courierID)
	}
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := likePattern(search)
		query = query.Where(
			"(code LIKE ? OR customer_name LIKE ? OR customer_phone LIKE ? OR pharmacy_queue_no LIKE ? OR dropoff_region LIKE ?)",
			like, like, like, like, like,
		)
	}
	query, err := filterCreatedDate(c, query)
	if err != nil {
		return err
	}
	// ?schedule=asap|today|later: delivery schedule (asap = no date set).
	today := time.Now().Format("2006-01-02")
	switch c.Query("schedule") {
	case "asap":
		query = query.Where("scheduled_date IS NULL")
	case "today":
		query = query.Where("scheduled_date = ?", today)
	case "later":
		query = query.Where("scheduled_date > ?", today)
	}
	query = query.Session(&gorm.Session{})

	var statusRows []struct {
		Status enums.OrderStatus
		Count  int64
	}
	if err := query.Select("status, COUNT(*) AS count").Group("status").Scan(&statusRows).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	byStatus := fiber.Map{}
	for _, r := range statusRows {
		byStatus[string(r.Status)] = r.Count
	}

	if status := c.Query("status"); status != "" {
		query = query.Where("status = ?", status).Session(&gorm.Session{})
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	var orders []models.Order
	// Transaction: the list shows "Belum bayar" and the panel holds back confirming unpaid orders.
	err = orderPreload(query).Preload("Transaction").Order("created_at DESC, id DESC").Scopes(page.scope).Find(&orders).Error
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"data":    orders,
		"meta":    page.meta(total),
		"summary": fiber.Map{"by_status": byStatus},
	})
}

// Stats — GET /api/orders/stats: jumlah order per status + pendapatan dari
// order Done, dihitung di DB (bukan dari daftar yang sudah dipaginasi).
func (h *OrderHandler) Stats(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var rows []struct {
		Status  enums.OrderStatus
		Count   int64
		Revenue float64
	}
	err := h.DB.Model(&models.Order{}).
		Select("status, COUNT(*) AS count, COALESCE(SUM(price), 0) AS revenue").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	var total int64
	var revenue float64
	byStatus := fiber.Map{}
	for _, r := range rows {
		total += r.Count
		byStatus[string(r.Status)] = r.Count
		if r.Status == enums.OrderStatusDone {
			revenue = r.Revenue
		}
	}

	// Delivered since midnight (the farmasi dashboard tile).
	y, m, d := time.Now().Date()
	var doneToday int64
	if err := h.DB.Model(&models.Order{}).
		Where("status = ? AND delivered_at >= ?", enums.OrderStatusDone, time.Date(y, m, d, 0, 0, 0, 0, time.Local)).
		Count(&doneToday).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	days := c.QueryInt("days", 30)
	if days != 7 && days != 30 && days != 90 {
		days = 30
	}
	trend, err := h.statsTrend(user, days)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	data := fiber.Map{
		"total":      total,
		"by_status":  byStatus,
		"done_today": doneToday,
		"days":       days,
		"trend":      trend,
	}
	// Money is for Admin & Finance only; the other roles get the counts.
	if user.HasAnyRole(enums.RoleAdmin, enums.RoleFinance) {
		data["revenue"] = revenue
	} else if daily, ok := trend["daily"].([]fiber.Map); ok {
		for _, d := range daily {
			delete(d, "revenue")
		}
	}
	return c.JSON(fiber.Map{"data": data})
}

// statsTrend — data grafik dashboard untuk `days` hari terakhir (termasuk hari ini):
// order masuk & pendapatan (order Done, per tanggal selesai) per hari, plus
// order masuk di rentang itu per status.
func (h *OrderHandler) statsTrend(user *models.User, days int) (fiber.Map, error) {
	y, m, d := time.Now().Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	from := today.AddDate(0, 0, -(days - 1))
	base := func() *gorm.DB { return h.DB.Model(&models.Order{}) }

	type dayRow struct {
		Day   string
		Count int64
		Sum   float64
	}
	var created, delivered []dayRow
	if err := base().Select("DATE_FORMAT(created_at, '%Y-%m-%d') AS day, COUNT(*) AS count").
		Where("created_at >= ?", from).Group("day").Scan(&created).Error; err != nil {
		return nil, err
	}
	if err := base().Select("DATE_FORMAT(delivered_at, '%Y-%m-%d') AS day, COALESCE(SUM(price), 0) AS sum").
		Where("status = ? AND delivered_at >= ?", enums.OrderStatusDone, from).Group("day").Scan(&delivered).Error; err != nil {
		return nil, err
	}
	ordersByDay := map[string]int64{}
	for _, r := range created {
		ordersByDay[r.Day] = r.Count
	}
	revenueByDay := map[string]float64{}
	for _, r := range delivered {
		revenueByDay[r.Day] = r.Sum
	}
	// Every day in the range, including ones with no orders, so the x-axis has no holes.
	daily := make([]fiber.Map, 0, days)
	for d := from; !d.After(today); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		daily = append(daily, fiber.Map{"date": key, "orders": ordersByDay[key], "revenue": revenueByDay[key]})
	}

	var statusRows []struct {
		Status enums.OrderStatus
		Count  int64
	}
	if err := base().Select("status, COUNT(*) AS count").Where("created_at >= ?", from).
		Group("status").Scan(&statusRows).Error; err != nil {
		return nil, err
	}
	byStatus := fiber.Map{}
	for _, r := range statusRows {
		byStatus[string(r.Status)] = r.Count
	}

	return fiber.Map{
		"daily":     daily,
		"by_status": byStatus,
	}, nil
}

func (h *OrderHandler) Show(c *fiber.Ctx) error {
	var order models.Order
	err := orderPreload(h.DB).Preload("StatusHistory.ChangedByUser").Preload("Transaction").
		Preload("Delivery.DeliveredByUser").
		First(&order, "id = ?", c.Params("id")).Error
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}
	withPhotoURLs(h.Files, order.Delivery)
	withProofURL(h.Files, order.Transaction)

	return c.JSON(fiber.Map{"data": order})
}

// Labels — POST /api/orders/labels {ids}: data order LENGKAP (tidak disamarkan,
// lihat middleware.MaskPatientData) untuk potongan bukti pesanan yang dibawa
// pasien. Bagian label yang ditempel di paket tetap memakai data tersamar.
func (h *OrderHandler) Labels(c *fiber.Ctx) error {
	var req struct {
		IDs []uint `json:"ids"`
	}
	if err := c.BodyParser(&req); err != nil || len(req.IDs) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "pilih order yang mau dicetak")
	}
	if len(req.IDs) > 200 {
		return fiber.NewError(fiber.StatusBadRequest, "maksimal 200 label sekali cetak")
	}
	var orders []models.Order
	err := orderPreload(h.DB).Preload("Transaction").
		Where("id IN ? AND status <> ?", req.IDs, enums.OrderStatusCancelled).
		Order("id").Find(&orders).Error
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": orders})
}

type storeOrderRequest struct {
	ServiceID      uint    `json:"service_id" validate:"required"`
	CustomerName   string  `json:"customer_name" validate:"required"`
	CustomerPhone  string  `json:"customer_phone" validate:"required"`
	PickupAddress  string  `json:"pickup_address"` // optional: empty = defaultPickup
	DropoffAddress string  `json:"dropoff_address" validate:"required"`
	Notes          *string `json:"notes"`
	// Saved customers (see customerForOrder): pick one, or save the typed one.
	CustomerID   *uint `json:"customer_id"`
	SaveCustomer bool  `json:"save_customer"`
	orderExtras
	freeInput
	pharmacyInput
	regionInput
	PaymentMethod string `json:"payment_method"` // cash | noncash
}

// Store — POST /api/orders. Dibuat staff, mulai dari Menunggu konfirmasi seperti
// order publik: CCO baru boleh mengonfirmasi setelah pembayarannya lunas.
// Order buatan Farmasi langsung lompat ke Siap diambil begitu dikonfirmasi
// (obatnya sudah di farmasi), lihat transition.
func (h *OrderHandler) Store(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var req storeOrderRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	// A picked saved customer fills the form with masked data; use the real values.
	if req.CustomerID != nil {
		var saved models.Customer
		if h.DB.First(&saved, *req.CustomerID).Error == nil {
			if err := restoreMasked([]maskedField{
				{&req.CustomerName, saved.Name, privacy.Name, "Nama penerima"},
				{&req.CustomerPhone, saved.Phone, privacy.Phone, "Nomor HP"},
				{&req.DropoffAddress, saved.Address, privacy.Address, "Alamat tujuan"},
			}); err != nil {
				return err
			}
		}
	}
	if err := validateOrderFields(req.ServiceID, &req.CustomerName, &req.CustomerPhone,
		&req.PickupAddress, &req.DropoffAddress, req.Notes); err != nil {
		return err
	}

	var service models.Service
	if err := withTiers(h.DB).Where("id = ? AND is_active = ?", req.ServiceID, true).First(&service).Error; err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "layanan tidak aktif")
	}

	org, err := partnerRS(h.DB)
	if err != nil {
		return err
	}
	orgID := org.ID
	scheduledDate, scheduledSlot, err := req.schedule(nil)
	if err != nil {
		return err
	}
	patientName, medicalRecordNo, err := req.patient()
	if err != nil {
		return err
	}
	dropoffLat, dropoffLng, err := req.dropoffPoint()
	if err != nil {
		return err
	}
	paymentMethod, err := paymentMethodOf(req.PaymentMethod)
	if err != nil {
		return err
	}

	status, source, note := enums.OrderStatusPending, enums.OrderSourceCco, "Order dibuat, menunggu pembayaran"
	if user.RoleName() == enums.RoleFarmasi {
		source, note = enums.OrderSourceFarmasi, "Order dibuat farmasi, menunggu pembayaran"
	}

	code, err := services.GenerateOrderCode(h.DB)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	now := time.Now()
	order := models.Order{
		Code:            code,
		OrganizationID:  &orgID,
		CustomerName:    req.CustomerName,
		CustomerPhone:   req.CustomerPhone,
		PatientName:     patientName,
		MedicalRecordNo: medicalRecordNo,
		PickupAddress:   req.PickupAddress,
		DropoffAddress:  req.DropoffAddress,
		ServiceID:       &req.ServiceID,
		Status:          status,
		Source:          source,
		CreatedBy:       &user.ID,
		Price:           service.PriceBase,
		Notes:           req.Notes,
		ScheduledDate:   scheduledDate,
		ScheduledSlot:   scheduledSlot,
		DropoffLat:      dropoffLat,
		DropoffLng:      dropoffLng,
	}
	// Picked up at the RS: start from its map point.
	order.PickupLat, order.PickupLng = org.Lat, org.Lng
	// The region first: without a map point the distance is measured from its centre.
	if err := req.regionInput.apply(h.DB, &order, true, true); err != nil {
		return err
	}
	if err := applyPrice(c.UserContext(), h.DB, h.Router, service, &order); err != nil {
		return err
	}
	if err := req.pharmacyInput.apply(h.DB, &order, true); err != nil {
		return err
	}
	// Program ongkir gratis: the staff checked the patient's category & domicile.
	if err := req.freeInput.apply(&order); err != nil {
		return err
	}
	program := loadFreeProgram(h.DB)
	applyFree(program, &order, user.ID)
	if order.IsFree {
		note = "Order dibuat. " + freeNote(program, &order)
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		customerID, err := customerForOrder(tx, orgID, user.ID, &req, order.DropoffRegionCode)
		if err != nil {
			return err
		}
		order.CustomerID = customerID
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		if err := openTransaction(tx, &order, paymentMethod); err != nil {
			return err
		}
		if err := syncFreePayment(tx, &order, user.ID); err != nil {
			return err
		}
		return tx.Create(&models.OrderStatusHistory{
			OrderID:   order.ID,
			ToStatus:  status,
			ChangedBy: &user.ID,
			Note:      &note,
			ChangedAt: now,
		}).Error
	})
	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		return err
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	orderPreload(h.DB).First(&order, order.ID)
	publishOrderEvent(h.Hub, "order.created", &order, &user.ID)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": order})
}

type transitionRequest struct {
	Status enums.OrderStatus `json:"status" validate:"required"`
	Note   *string           `json:"note"`
	// Cancelling a paid order: "refund" (money back) or "keep" (no refund, reason required).
	Payment cancelPayment `json:"payment"`
}

const maxBulkOrders = 100

type bulkTransitionRequest struct {
	IDs     []uint            `json:"ids"`
	Status  enums.OrderStatus `json:"status"`
	Note    *string           `json:"note"`
	Payment cancelPayment     `json:"payment"`
}

type bulkFailure struct {
	ID    uint   `json:"id"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error"`
}

// BulkTransition — POST /api/orders/bulk-transition {ids, status, note}
// Tiap order diproses sendiri-sendiri pakai aturan yang sama dengan Transition;
// yang gagal tidak membatalkan yang lain. Response: {data: {updated, failed}}.
func (h *OrderHandler) BulkTransition(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var req bulkTransitionRequest
	if err := c.BodyParser(&req); err != nil || !req.Status.Valid() {
		return fiber.NewError(fiber.StatusBadRequest, "status tidak valid")
	}
	return h.eachOrder(c, user, req.IDs, func(order *models.Order) error {
		return h.transition(user, order, req.Status, req.Note, req.Payment)
	})
}

type bulkAssignRequest struct {
	IDs       []uint `json:"ids"`
	CourierID uint   `json:"courier_id"`
}

// BulkAssignCourier — POST /api/orders/bulk-assign-courier {ids, courier_id}
// Aturan sama dengan AssignCourier (antara
// Dikonfirmasi dan Siap kirim). Response: {data: {updated, failed}}.
func (h *OrderHandler) BulkAssignCourier(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var req bulkAssignRequest
	if err := c.BodyParser(&req); err != nil || req.CourierID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "courier_id wajib diisi")
	}

	var courier models.Courier
	if err := h.DB.Preload("User").First(&courier, req.CourierID).Error; err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "kurir tidak ditemukan")
	}

	return h.eachOrder(c, user, req.IDs, func(order *models.Order) error {
		return h.Orders.AssignCourier(order, &courier, &user.ID)
	})
}

// eachOrder runs fn on every (deduplicated) order in ids the user may see, and
// responds with which succeeded and which failed (with the reason). One failure
// doesn't stop the rest.
func (h *OrderHandler) eachOrder(c *fiber.Ctx, user *models.User, ids []uint, fn func(*models.Order) error) error {
	if len(ids) == 0 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "pilih minimal 1 order")
	}
	if len(ids) > maxBulkOrders {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "maksimal 100 order sekali proses")
	}

	var orders []models.Order
	if err := h.DB.Where("id IN ?", ids).Find(&orders).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	byID := make(map[uint]*models.Order, len(orders))
	for i := range orders {
		byID[orders[i].ID] = &orders[i]
	}

	updated := []uint{}
	failed := []bulkFailure{}
	seen := map[uint]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true

		order, ok := byID[id]
		if !ok {
			failed = append(failed, bulkFailure{ID: id, Error: "order tidak ditemukan"})
			continue
		}
		if err := fn(order); err != nil {
			failed = append(failed, bulkFailure{ID: id, Code: order.Code, Error: err.Error()})
			continue
		}
		updated = append(updated, id)
	}

	return c.JSON(fiber.Map{"data": fiber.Map{"updated": updated, "failed": failed}})
}

// Transition — POST /api/orders/:id/transition
func (h *OrderHandler) Transition(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var order models.Order
	if err := h.DB.First(&order, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}

	var req transitionRequest
	if err := c.BodyParser(&req); err != nil || !req.Status.Valid() {
		return fiber.NewError(fiber.StatusBadRequest, "status tidak valid")
	}

	if err := h.transition(user, &order, req.Status, req.Note, req.Payment); err != nil {
		var fiberErr *fiber.Error
		if errors.As(err, &fiberErr) {
			return err
		}
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}

	orderPreload(h.DB).First(&order, order.ID)
	return c.JSON(fiber.Map{"data": order})
}

// transition moves an order on behalf of a staff member: each step belongs to
// one role (enums.StaffCanMove — e.g. only farmasi marks "siap diambil"), then
// whoever must act next is notified.
func (h *OrderHandler) transition(user *models.User, order *models.Order, to enums.OrderStatus, note *string, payment cancelPayment) error {
	if order.Status.CanTransitionTo(to) && !order.Status.StaffCanMove(user.RoleName(), to) {
		return fiber.NewError(fiber.StatusForbidden, fmt.Sprintf("role kamu tidak bisa mengubah status %s ke %s",
			order.Status.Label(), to.Label()))
	}
	// The CCO confirms only paid orders, so the medicine is never prepared for
	// nothing; checked again before it leaves the RS (it may have been refunded).
	if to == enums.OrderStatusConfirmed || to == enums.OrderStatusPacked {
		if err := requirePaid(h.DB, order); err != nil {
			return err
		}
	}
	// A paid order can't just vanish: the canceller says whether the money goes back.
	var paidTxn *models.Transaction
	if to == enums.OrderStatusCancelled {
		var err error
		if paidTxn, err = checkCancelPayment(h.DB, order, payment, note); err != nil {
			return err
		}
	}
	if err := h.Orders.Transition(order, to, &user.ID, note); err != nil {
		return err
	}
	if paidTxn != nil && payment == cancelRefund {
		if err := refundOnCancel(h.DB, paidTxn, user, note); err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "order dibatalkan, tapi refund gagal dicatat: "+err.Error())
		}
	}
	// Made by Farmasi: the medicine is already there, so a confirmed order is ready right away.
	if to == enums.OrderStatusConfirmed && order.Source == enums.OrderSourceFarmasi {
		ready := "Obat sudah di farmasi (order dibuat farmasi)"
		if err := h.Orders.Transition(order, enums.OrderStatusReadyForPickup, &user.ID, &ready); err != nil {
			return err
		}
		h.DB.Preload("Organization").First(order, order.ID)
		// Skipped "Dikonfirmasi" (which tells Ops): CCO collects, Ops assigns a courier.
		publishOrderEvent(h.Hub, "order.status", order, &user.ID, enums.RoleCco, enums.RoleOps)
		return nil
	}
	h.DB.Preload("Organization").First(order, order.ID)
	publishOrderEvent(h.Hub, "order.status", order, &user.ID)
	return nil
}

type assignCourierRequest struct {
	CourierID uint `json:"courier_id" validate:"required"`
}

// AssignCourier — POST /api/orders/:id/assign-courier
func (h *OrderHandler) AssignCourier(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var order models.Order
	if err := h.DB.First(&order, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}

	var req assignCourierRequest
	if err := c.BodyParser(&req); err != nil || req.CourierID == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "courier_id wajib diisi")
	}

	var courier models.Courier
	if err := h.DB.Preload("User").First(&courier, req.CourierID).Error; err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "kurir tidak ditemukan")
	}

	if err := h.Orders.AssignCourier(&order, &courier, &user.ID); err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}

	orderPreload(h.DB).First(&order, order.ID)
	return c.JSON(fiber.Map{"data": order})
}

// AssignableCouriers — GET /api/orders/:id/assignable-couriers
// List kurir aktif yang bisa di-assign ke order ini.
func (h *OrderHandler) AssignableCouriers(c *fiber.Ctx) error {
	var order models.Order
	if err := h.DB.First(&order, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}

	var couriers []models.Courier
	if err := h.DB.Preload("User").Where("is_active = ?", true).Find(&couriers).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{"data": couriers})
}
