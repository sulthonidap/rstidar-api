package handlers

import (
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
	"medsend-api/internal/privacy"
	"medsend-api/internal/realtime"
	"medsend-api/internal/routing"
	"medsend-api/internal/services"
	"medsend-api/internal/storage"
)

// PublicHandler — endpoint tanpa login: landing data, submit order publik,
// dan tracking. Porting dari PublicOrderController.php.
type PublicHandler struct {
	DB     *gorm.DB
	Hub    *realtime.Hub
	Store  storage.Storage
	Router routing.Router // road distances for pricing (nil = straight-line estimate)
}

func (h *PublicHandler) Services(c *fiber.Ctx) error {
	var list []models.Service
	withTiers(h.DB).Where("is_active = ?", true).Order("price_base").Find(&list)
	return c.JSON(fiber.Map{"data": list})
}

var phonePattern = regexp.MustCompile(`^[0-9+\-\s()]{6,}$`)

// defaultPickup: where the courier collects the package when the form doesn't say.
const defaultPickup = "Farmasi"

// validateOrderFields trims the text fields in place and applies the same rules
// to public and staff orders. An empty pickup becomes defaultPickup.
func validateOrderFields(serviceID uint, name, phone, pickup, dropoff *string, notes *string) error {
	for _, s := range []*string{name, phone, pickup, dropoff} {
		*s = strings.TrimSpace(*s)
	}
	if *pickup == "" {
		*pickup = defaultPickup
	}
	if serviceID == 0 || *name == "" || *phone == "" || *pickup == "" || *dropoff == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "field wajib belum lengkap")
	}
	if len(*name) > 255 || len(*pickup) > 1000 || len(*dropoff) > 1000 || (notes != nil && len(*notes) > 1000) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "isian terlalu panjang")
	}
	if len(*phone) > 30 || !phonePattern.MatchString(*phone) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "nomor telepon tidak valid")
	}
	return nil
}

type publicOrderRequest struct {
	ServiceID      uint    `json:"service_id" validate:"required"`
	CustomerName   string  `json:"customer_name" validate:"required"`
	CustomerPhone  string  `json:"customer_phone" validate:"required"`
	PickupAddress  string  `json:"pickup_address"` // optional: empty = defaultPickup
	DropoffAddress string  `json:"dropoff_address" validate:"required"`
	Notes          *string `json:"notes"`
	Website        string  `json:"website"` // honeypot — harus kosong
	// Destination map point (optional, e.g. "gunakan lokasi saya").
	DropoffLat *float64 `json:"dropoff_lat"`
	DropoffLng *float64 `json:"dropoff_lng"`
	// Optional, as on the staff form: the patient (when not the recipient) and
	// their medical record number at the RS.
	PatientName     *string `json:"patient_name"`
	MedicalRecordNo *string `json:"medical_record_no"`
	// Farmasi: asked (and required) only while the Admin turned it on.
	pharmacyInput
	// cash (tunai di kasir RS) | noncash (transfer/QRIS, dicek CCO)
	PaymentMethod string `json:"payment_method"`
}

// StoreOrder — POST /api/public/orders: an order for the partner RS, from the
// public form.
func (h *PublicHandler) StoreOrder(c *fiber.Ctx) error {
	var req publicOrderRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}

	if req.Website != "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "terdeteksi spam")
	}
	if err := validateOrderFields(req.ServiceID, &req.CustomerName, &req.CustomerPhone,
		&req.PickupAddress, &req.DropoffAddress, req.Notes); err != nil {
		return err
	}
	if !validPoint(req.DropoffLat, req.DropoffLng) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "titik tujuan harus di wilayah Indonesia")
	}
	patientName, medicalRecordNo, err := (&orderExtras{PatientName: req.PatientName, MedicalRecordNo: req.MedicalRecordNo}).patient()
	if err != nil {
		return err
	}
	paymentMethod, err := paymentMethodOf(req.PaymentMethod)
	if err != nil {
		return err
	}

	org, err := partnerRS(h.DB)
	if err != nil {
		return err
	}

	var service models.Service
	if err := withTiers(h.DB).Where("id = ? AND is_active = ?", req.ServiceID, true).First(&service).Error; err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "layanan tidak aktif")
	}

	code, err := services.GenerateOrderCode(h.DB)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	order := models.Order{
		Code:            code,
		OrganizationID:  &org.ID,
		CustomerName:    req.CustomerName,
		CustomerPhone:   req.CustomerPhone,
		PickupAddress:   req.PickupAddress,
		DropoffAddress:  req.DropoffAddress,
		ServiceID:       &service.ID,
		Status:          enums.OrderStatusPending,
		Source:          enums.OrderSourcePublic,
		Price:           service.PriceBase,
		Notes:           req.Notes,
		DropoffLat:      req.DropoffLat,
		DropoffLng:      req.DropoffLng,
		PatientName:     patientName,
		MedicalRecordNo: medicalRecordNo,
		// Picked up at the RS: start from its map point.
		PickupLat: org.Lat,
		PickupLng: org.Lng,
	}
	if pharmacyOnPublicForm(h.DB) {
		if err := req.pharmacyInput.apply(h.DB, &order, true); err != nil {
			return err
		}
	}
	if err := applyPrice(c.UserContext(), h.DB, h.Router, service, &order); err != nil {
		return err
	}

	err = h.DB.Transaction(func(tx *gorm.DB) error {
		// The patient goes into the customer list, so staff can reuse their details
		// for the next order. Best effort: never lose the order over it.
		if id, err := rememberCustomer(tx, org.ID, nil, customerContact{
			Name: order.CustomerName, Phone: order.CustomerPhone, Address: order.DropoffAddress,
			Lat: order.DropoffLat, Lng: order.DropoffLng,
		}); err == nil {
			order.CustomerID = id
		}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		if err := openTransaction(tx, &order, paymentMethod); err != nil {
			return err
		}
		note := "Order dibuat via form publik"
		return tx.Create(&models.OrderStatusHistory{
			OrderID:   order.ID,
			ToStatus:  enums.OrderStatusPending,
			Note:      &note,
			ChangedAt: order.CreatedAt,
		}).Error
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	order.Organization = &org
	publishOrderEvent(h.Hub, "order.created", &order, nil)

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": fiber.Map{"code": order.Code}})
}

// Organization — GET /api/public/organization: the partner RS, for the order
// form header and the distance shown on its map.
func (h *PublicHandler) Organization(c *fiber.Ctx) error {
	org, err := partnerRS(h.DB)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": fiber.Map{
		"name":    org.Name,
		"address": org.Address,
		"lat":     org.Lat,
		"lng":     org.Lng,
	}})
}

// Track — GET /api/public/orders/:code
// Tanpa login dan kodenya bisa ditebak, jadi cuma kirim yang ditampilkan halaman
// lacak (sama seperti versi Laravel): HP disamarkan, tanpa alamat pickup, tanpa
// data user staff/kurir selain nama kurir, tanpa ID internal.
func (h *PublicHandler) Track(c *fiber.Ctx) error {
	var order models.Order
	err := h.DB.Preload("Organization").Preload("Service").Preload("Courier.User").Preload("Delivery").Preload("Transaction").
		Preload("StatusHistory", func(db *gorm.DB) *gorm.DB { return db.Order("changed_at DESC, id DESC") }).
		First(&order, "code = ?", c.Params("code")).Error
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}

	var org fiber.Map
	if order.Organization != nil {
		org = fiber.Map{"name": order.Organization.Name}
	}
	var serviceName, courierName *string
	if order.Service != nil {
		serviceName = &order.Service.Name
	}
	if order.Courier != nil && order.Courier.User != nil {
		courierName = &order.Courier.User.Name
	}
	history := make([]fiber.Map, 0, len(order.StatusHistory))
	for _, s := range order.StatusHistory {
		history = append(history, fiber.Map{
			"from_status": s.FromStatus,
			"to_status":   s.ToStatus,
			"note":        s.Note,
			"changed_at":  s.ChangedAt,
		})
	}

	return c.JSON(fiber.Map{"data": fiber.Map{
		"code":            order.Code,
		"status":          order.Status,
		"organization":    org,
		"service_name":    serviceName,
		"price":           order.Price,
		"is_free":         order.IsFree, // program ongkir gratis (lansia/disabilitas)
		"customer_name":   privacy.Name(order.CustomerName),
		"customer_phone":  maskPhone(order.CustomerPhone),
		"dropoff_address": privacy.Address(order.DropoffAddress),
		"courier_name":    courierName,
		"scheduled_date":  order.ScheduledDate,
		"scheduled_slot":  order.ScheduledSlot,
		"created_at":      order.CreatedAt,
		"status_history":  history,
		"delivery":        publicDelivery(h.Store, order.Delivery),
		"payment":         publicPayment(order.Transaction),
	}})
}

// publicDelivery: the proof of delivery for the tracking page — photos, who
// received it and when, but not the GPS point (it's the recipient's home).
func publicDelivery(store storage.Storage, d *models.OrderDelivery) fiber.Map {
	if d == nil {
		return nil
	}
	withPhotoURLs(store, d)
	return fiber.Map{
		"receiver_name":     d.ReceiverName,
		"receiver_relation": d.ReceiverRelation,
		"photo_urls":        d.PhotoURLs,
		"thumb_urls":        d.ThumbURLs,
		"delivered_at":      d.DeliveredAt,
	}
}

// maskPhone keeps the first 3 and last 2 digits: 081234567890 → 081*******90.
func maskPhone(phone string) string {
	r := []rune(phone)
	if len(r) <= 5 {
		return phone
	}
	for i := 3; i < len(r)-2; i++ {
		r[i] = '*'
	}
	return string(r)
}
