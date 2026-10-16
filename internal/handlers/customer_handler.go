package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

// CustomerHandler — saved recipients of the partner RS.
type CustomerHandler struct {
	DB *gorm.DB
}

// Index — GET /api/customers?search=&page=&per_page=
// search matches name or phone (used by the order form's autocomplete too).
func (h *CustomerHandler) Index(c *fiber.Ctx) error {
	page := parsePage(c)

	query := h.DB.Model(&models.Customer{})
	if search := strings.TrimSpace(c.Query("search")); search != "" {
		like := likePattern(search)
		phone := like
		if p := normalizePhone(search); p != "" {
			phone = likePattern(p) // phones are stored as 08…, whatever is typed
		}
		query = query.Where("(name LIKE ? OR phone LIKE ?)", like, phone)
	}
	query = query.Session(&gorm.Session{})

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	list := []models.Customer{}
	err := query.Scopes(page.scope).
		Select("customers.*, " +
			"(SELECT COUNT(*) FROM orders o WHERE o.customer_id = customers.id) AS order_count, " +
			"(SELECT MAX(o.created_at) FROM orders o WHERE o.customer_id = customers.id) AS last_order_at").
		Order("name").
		Find(&list).Error
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": list, "meta": page.meta(total)})
}

type customerRequest struct {
	Name    string   `json:"name"`
	Phone   string   `json:"phone"`
	Address string   `json:"address"`
	Notes   *string  `json:"notes"`
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`
}

func (r *customerRequest) validate() error {
	if !validPoint(r.Lat, r.Lng) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "titik alamat harus di wilayah Indonesia")
	}
	r.Name = strings.TrimSpace(r.Name)
	r.Phone = strings.TrimSpace(r.Phone)
	r.Address = strings.TrimSpace(r.Address)
	if r.Notes != nil {
		if n := strings.TrimSpace(*r.Notes); n == "" {
			r.Notes = nil
		} else {
			r.Notes = &n
		}
	}
	if r.Name == "" || r.Phone == "" || r.Address == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "nama, nomor HP, dan alamat wajib diisi")
	}
	if len(r.Name) > 255 || len(r.Address) > 1000 || (r.Notes != nil && len(*r.Notes) > 1000) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "isian terlalu panjang")
	}
	if len(r.Phone) > 30 || !phonePattern.MatchString(r.Phone) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "nomor telepon tidak valid")
	}
	r.Phone = normalizePhone(r.Phone)
	return nil
}

// rsPoint is the RS's map point, used as the pickup point of its orders
// (nil, nil when the admin hasn't set it yet).
func rsPoint(db *gorm.DB, orgID *uint) (*float64, *float64) {
	if orgID == nil {
		return nil, nil
	}
	var org models.Organization
	if err := db.Select("lat, lng").First(&org, *orgID).Error; err != nil {
		return nil, nil
	}
	return org.Lat, org.Lng
}

// customerTaken reports a 409 when the RS already has a customer with this
// name and phone (one phone may be shared by several people with other names).
func customerTaken(db *gorm.DB, orgID uint, name, phone string, exceptID uint) error {
	var other models.Customer
	err := db.Where("organization_id = ? AND phone = ? AND name = ? AND id <> ?", orgID, phone, name, exceptID).First(&other).Error
	if err == nil {
		return fiber.NewError(fiber.StatusConflict, "Customer "+other.Name+" dengan nomor HP ini sudah ada.")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return nil
}

func (h *CustomerHandler) Store(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)
	var req customerRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if err := req.validate(); err != nil {
		return err
	}
	org, err := partnerRS(h.DB)
	if err != nil {
		return err
	}
	if err := customerTaken(h.DB, org.ID, req.Name, req.Phone, 0); err != nil {
		return err
	}

	customer := models.Customer{
		OrganizationID: org.ID,
		Name:           req.Name,
		Phone:          req.Phone,
		Address:        req.Address,
		Lat:            req.Lat,
		Lng:            req.Lng,
		Notes:          req.Notes,
		CreatedBy:      &user.ID,
	}
	if err := h.DB.Create(&customer).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": customer})
}

func (h *CustomerHandler) find(c *fiber.Ctx) (*models.Customer, error) {
	var customer models.Customer
	if err := h.DB.First(&customer, "id = ?", c.Params("id")).Error; err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "customer tidak ditemukan")
	}
	return &customer, nil
}

// Update — PUT /api/customers/:id. Past orders keep
// the details they were made with.
func (h *CustomerHandler) Update(c *fiber.Ctx) error {
	customer, err := h.find(c)
	if err != nil {
		return err
	}
	var req customerRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if err := req.validate(); err != nil {
		return err
	}
	if err := customerTaken(h.DB, customer.OrganizationID, req.Name, req.Phone, customer.ID); err != nil {
		return err
	}

	customer.Name, customer.Phone, customer.Address, customer.Notes = req.Name, req.Phone, req.Address, req.Notes
	customer.Lat, customer.Lng = req.Lat, req.Lng
	if err := h.DB.Save(customer).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": customer})
}

// Destroy — DELETE /api/customers/:id. Orders keep their own copy of the
// customer's details, so deleting only unlinks them.
func (h *CustomerHandler) Destroy(c *fiber.Ctx) error {
	customer, err := h.find(c)
	if err != nil {
		return err
	}
	if err := h.DB.Delete(customer).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// normalizePhone writes a phone number the same way however it was typed
// ("+62 812-3456", "62812 3456", "0812 3456" → "08123456"), so a customer is
// found again whatever format the next order uses.
func normalizePhone(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if strings.HasPrefix(d, "62") {
		d = "0" + d[2:]
	}
	return d
}

// customerContact: an order's recipient as it goes into the customer list.
type customerContact struct {
	Name, Phone, Address string
	Lat, Lng             *float64
}

// rememberCustomer finds the RS's customer with the same phone AND name — one
// phone can be shared by a family (a child ordering for both parents), so the
// phone alone isn't a person — and refreshes their address/point, or adds them.
func rememberCustomer(tx *gorm.DB, orgID uint, createdBy *uint, ct customerContact) (*uint, error) {
	name, phone := strings.TrimSpace(ct.Name), normalizePhone(ct.Phone)
	if name == "" || phone == "" {
		return nil, nil
	}

	var customer models.Customer
	err := tx.Where("organization_id = ? AND phone = ? AND name = ?", orgID, phone, name).First(&customer).Error
	switch {
	case err == nil:
		customer.Address = ct.Address
		if ct.Lat != nil {
			customer.Lat, customer.Lng = ct.Lat, ct.Lng
		}
		err = tx.Save(&customer).Error
	case errors.Is(err, gorm.ErrRecordNotFound):
		customer = models.Customer{
			OrganizationID: orgID,
			Name:           name,
			Phone:          phone,
			Address:        ct.Address,
			Lat:            ct.Lat,
			Lng:            ct.Lng,
			CreatedBy:      createdBy,
		}
		err = tx.Create(&customer).Error
	}
	if err != nil {
		return nil, err
	}
	return &customer.ID, nil
}

// customerForOrder links a staff order to a saved customer:
//   - customer_id set: that customer;
//   - save_customer: saves the typed details (see rememberCustomer), so next
//     time they can be picked from the list.
//
// Returns nil when neither applies.
func customerForOrder(tx *gorm.DB, orgID uint, userID uint, req *storeOrderRequest) (*uint, error) {
	if req.CustomerID != nil {
		var customer models.Customer
		if err := tx.Where("id = ? AND organization_id = ?", *req.CustomerID, orgID).First(&customer).Error; err != nil {
			return nil, fiber.NewError(fiber.StatusUnprocessableEntity, "customer tidak ditemukan")
		}
		return &customer.ID, nil
	}
	if !req.SaveCustomer {
		return nil, nil
	}
	return rememberCustomer(tx, orgID, &userID, customerContact{
		Name: req.CustomerName, Phone: req.CustomerPhone, Address: req.DropoffAddress,
		Lat: req.DropoffLat, Lng: req.DropoffLng,
	})
}
