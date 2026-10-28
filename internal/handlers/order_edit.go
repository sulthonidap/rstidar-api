package handlers

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
	"medsend-api/internal/privacy"
)

// editableStatuses: an order can be corrected until the courier has the package.
var editableStatuses = map[enums.OrderStatus]bool{
	enums.OrderStatusPending:        true,
	enums.OrderStatusConfirmed:      true,
	enums.OrderStatusReadyForPickup: true,
	enums.OrderStatusPacked:         true,
}

type updateOrderRequest struct {
	ServiceID      uint    `json:"service_id"`
	CustomerName   string  `json:"customer_name"`
	CustomerPhone  string  `json:"customer_phone"`
	PickupAddress  string  `json:"pickup_address"`
	DropoffAddress string  `json:"dropoff_address"`
	Notes          *string `json:"notes"`
	orderExtras
	freeInput
}

// Update — PUT /api/orders/:id (CCO/Admin). Corrects the order's details
// until the courier has the package. A changed address drops its map point
// (it pointed at the old address); a changed service re-prices the order.
// The change is written to the status history ("Diubah: …").
func (h *OrderHandler) Update(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)

	var order models.Order
	if err := h.DB.First(&order, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}
	if !editableStatuses[order.Status] {
		return fiber.NewError(fiber.StatusUnprocessableEntity,
			"order yang sudah "+strings.ToLower(order.Status.Label())+" tidak bisa diubah")
	}

	var req updateOrderRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if strings.TrimSpace(req.PickupAddress) == "" {
		req.PickupAddress = order.PickupAddress // not on the form: keep it
	}
	// The form shows masked patient data; unchanged fields keep the real value.
	if err := restoreMasked([]maskedField{
		{&req.CustomerName, order.CustomerName, privacy.Name, "Nama penerima"},
		{&req.CustomerPhone, order.CustomerPhone, privacy.Phone, "Nomor HP"},
		{&req.DropoffAddress, order.DropoffAddress, privacy.Address, "Alamat tujuan"},
		{req.PatientName, derefString(order.PatientName), privacy.Name, "Nama pasien"},
		{req.MedicalRecordNo, derefString(order.MedicalRecordNo), privacy.MedicalRecordNo, "No. RM"},
	}); err != nil {
		return err
	}
	if err := validateOrderFields(req.ServiceID, &req.CustomerName, &req.CustomerPhone,
		&req.PickupAddress, &req.DropoffAddress, req.Notes); err != nil {
		return err
	}
	if req.Notes != nil {
		if n := strings.TrimSpace(*req.Notes); n == "" {
			req.Notes = nil
		} else {
			req.Notes = &n
		}
	}

	scheduledDate, scheduledSlot, err := req.schedule(order.ScheduledDate)
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

	changed := []string{} // [] (not null) when nothing changed
	if req.ServiceID != derefUint(order.ServiceID) {
		var service models.Service
		if err := h.DB.Where("id = ? AND is_active = ?", req.ServiceID, true).First(&service).Error; err != nil {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "layanan tidak aktif")
		}
		order.ServiceID = &service.ID
		changed = append(changed, "layanan")
	}
	if req.CustomerName != order.CustomerName {
		order.CustomerName = req.CustomerName
		changed = append(changed, "nama penerima")
	}
	if req.CustomerPhone != order.CustomerPhone {
		order.CustomerPhone = req.CustomerPhone
		changed = append(changed, "nomor HP")
	}
	if req.PickupAddress != order.PickupAddress {
		order.PickupAddress = req.PickupAddress
		// The old point was for the old place; go back to the RS point.
		order.PickupLat, order.PickupLng = rsPoint(h.DB, order.OrganizationID)
		changed = append(changed, "lokasi pickup")
	}
	if req.DropoffAddress != order.DropoffAddress {
		order.DropoffAddress = req.DropoffAddress
		changed = append(changed, "alamat tujuan")
	}
	// The form sends the destination point it shows (null = none).
	if !samePoint(dropoffLat, dropoffLng, order.DropoffLat, order.DropoffLng) {
		order.DropoffLat, order.DropoffLng = dropoffLat, dropoffLng
		changed = append(changed, "titik tujuan")
	}
	if derefString(req.Notes) != derefString(order.Notes) {
		order.Notes = req.Notes
		changed = append(changed, "catatan")
	}
	if !sameDate(scheduledDate, order.ScheduledDate) || !sameSlot(scheduledSlot, order.ScheduledSlot) {
		order.ScheduledDate, order.ScheduledSlot = scheduledDate, scheduledSlot
		changed = append(changed, "jadwal kirim")
	}
	if derefString(patientName) != derefString(order.PatientName) || derefString(medicalRecordNo) != derefString(order.MedicalRecordNo) {
		order.PatientName, order.MedicalRecordNo = patientName, medicalRecordNo
		changed = append(changed, "data pasien")
	}
	// Program ongkir gratis: CCO verifies the patient (e.g. on a public-form order).
	oldCategory, oldChecked, wasFree := order.PatientCategory, order.DomicileChecked, order.IsFree
	if err := req.freeInput.apply(&order); err != nil {
		return err
	}
	if order.PatientCategory != oldCategory || order.DomicileChecked != oldChecked {
		changed = append(changed, "kategori pasien")
	}
	program := loadFreeProgram(h.DB)

	// Re-price: the service, the destination point or the RS point may have changed.
	var service models.Service
	if order.ServiceID != nil && withTiers(h.DB).First(&service, *order.ServiceID).Error == nil {
		oldPrice := order.Price
		if err := applyPrice(c.UserContext(), h.DB, h.Router, service, &order); err != nil {
			return err
		}
		applyFree(program, &order, user.ID)
		if order.Price != oldPrice {
			// The patient paid the old price; a new one needs a refund first
			// (an order settled at Rp0 because it was free isn't "paid by the patient").
			if paidByPatient(h.DB, order.ID) {
				return fiber.NewError(fiber.StatusUnprocessableEntity,
					fmt.Sprintf("order sudah lunas %s, perubahan ini membuat harga jadi %s. Refund dulu kalau layanan/alamat memang perlu diganti",
						rupiah(oldPrice), rupiah(order.Price)))
			}
			changed = append(changed, "harga")
		}
	}

	if len(changed) > 0 {
		note := "Diubah: " + strings.Join(changed, ", ")
		switch {
		case order.IsFree && !wasFree:
			note += ". " + freeNote(program, &order)
		case !order.IsFree && wasFree:
			note += ". Ongkir tidak lagi gratis"
		}
		err := h.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Save(&order).Error; err != nil {
				return err
			}
			if err := syncFreePayment(tx, &order, user.ID); err != nil {
				return err
			}
			// Same from/to status marks an edit, not a step.
			return tx.Create(&models.OrderStatusHistory{
				OrderID: order.ID, FromStatus: &order.Status, ToStatus: order.Status,
				ChangedBy: &user.ID, Note: &note, ChangedAt: time.Now(),
			}).Error
		})
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
	}

	syncTransactionAmount(h.DB, &order)
	orderPreload(h.DB).First(&order, order.ID)
	return c.JSON(fiber.Map{"data": order, "changed": changed})
}

type coordinatesRequest struct {
	PickupLat  *float64 `json:"pickup_lat"`
	PickupLng  *float64 `json:"pickup_lng"`
	DropoffLat *float64 `json:"dropoff_lat"`
	DropoffLng *float64 `json:"dropoff_lng"`
}

// Coordinates — PUT /api/orders/:id/coordinates (CCO/Ops/Admin). Saves the
// map points placed by hand when the address search misses or is off; the
// courier app navigates to them. null clears a point (back to address search).
func (h *OrderHandler) Coordinates(c *fiber.Ctx) error {
	var order models.Order
	if err := h.DB.First(&order, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}
	if order.Status.IsFinal() {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "order sudah selesai/batal")
	}

	var req coordinatesRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if !validPoint(req.PickupLat, req.PickupLng) || !validPoint(req.DropoffLat, req.DropoffLng) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "titik peta harus di wilayah Indonesia")
	}

	order.PickupLat, order.PickupLng = req.PickupLat, req.PickupLng
	order.DropoffLat, order.DropoffLng = req.DropoffLat, req.DropoffLng
	// A moved point changes the distance, so the price — unless it's paid: fixing
	// a misplaced pin then keeps the price the patient already paid.
	var service models.Service
	if !isPaid(h.DB, order.ID) && order.ServiceID != nil && withTiers(h.DB).First(&service, *order.ServiceID).Error == nil {
		if err := applyPrice(c.UserContext(), h.DB, h.Router, service, &order); err != nil {
			return err
		}
	}
	err := h.DB.Model(&order).Select("pickup_lat", "pickup_lng", "dropoff_lat", "dropoff_lng", "price", "distance_km", "distance_source").Updates(models.Order{
		PickupLat: order.PickupLat, PickupLng: order.PickupLng,
		DropoffLat: order.DropoffLat, DropoffLng: order.DropoffLng,
		Price: order.Price, DistanceKm: order.DistanceKm, DistanceSource: order.DistanceSource,
	}).Error
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	syncTransactionAmount(h.DB, &order)

	orderPreload(h.DB).First(&order, order.ID)
	return c.JSON(fiber.Map{"data": order})
}

// validPoint: both set and roughly inside Indonesia, or both empty.
func validPoint(lat, lng *float64) bool {
	if lat == nil || lng == nil {
		return lat == nil && lng == nil
	}
	return *lat >= -11.5 && *lat <= 6.5 && *lng >= 94 && *lng <= 141.5
}

func derefUint(p *uint) uint {
	if p == nil {
		return 0
	}
	return *p
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
