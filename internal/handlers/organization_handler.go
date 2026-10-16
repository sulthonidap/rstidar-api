package handlers

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/license"
	"medsend-api/internal/models"
	"medsend-api/internal/services"
)

// This edition serves a single partner RS: the one organizations row, filled
// in by the admin on first login (Setup Profil RS) and named after the license.
// Every order, customer and transaction belongs to it.

var errNoPartner = fiber.NewError(fiber.StatusConflict, "Profil RS belum diisi. Admin perlu melengkapinya dulu.")

// partnerRS is the RS this deployment serves; errNoPartner until it's set up.
func partnerRS(db *gorm.DB) (models.Organization, error) {
	var org models.Organization
	if err := db.Order("id").First(&org).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return org, errNoPartner
		}
		return org, fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return org, nil
}

type OrganizationHandler struct {
	DB *gorm.DB
}

// Show — GET /api/organization: the partner RS profile; data null until the
// admin has set it up (the panel then sends the admin to the setup page).
func (h *OrganizationHandler) Show(c *fiber.Ctx) error {
	org, err := partnerRS(h.DB)
	if errors.Is(err, errNoPartner) {
		return c.JSON(fiber.Map{"data": nil})
	}
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": org})
}

// organizationRequest: the editable profile. The name comes from the license.
type organizationRequest struct {
	Address *string `json:"address"`
	Phone   *string `json:"phone"`
	// Map point; both null clears it.
	Lat *float64 `json:"lat"`
	Lng *float64 `json:"lng"`
	// Service area in km from the RS (null = no limit).
	MaxDistanceKm *float64 `json:"max_distance_km"`
}

func (r *organizationRequest) validate() error {
	if !validPoint(r.Lat, r.Lng) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "titik lokasi RS harus di wilayah Indonesia")
	}
	if m := r.MaxDistanceKm; m != nil && (*m <= 0 || *m > 1000) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "jarak maksimal harus antara 0 dan 1000 km")
	}
	return nil
}

func (r *organizationRequest) applyTo(org *models.Organization, lic license.Info) {
	org.Name = lic.RS
	org.Address = r.Address
	org.Phone = r.Phone
	org.Lat, org.Lng = r.Lat, r.Lng
	org.MaxDistanceKm = r.MaxDistanceKm
}

// Setup — POST /api/organization (Admin): creates the partner RS, once.
func (h *OrganizationHandler) Setup(c *fiber.Ctx) error {
	lic, err := activeLicense(h.DB)
	if err != nil {
		return err
	}
	if _, err := partnerRS(h.DB); !errors.Is(err, errNoPartner) {
		if err != nil {
			return err
		}
		return fiber.NewError(fiber.StatusConflict, "profil RS sudah ada")
	}

	var req organizationRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if err := req.validate(); err != nil {
		return err
	}

	org := models.Organization{Slug: services.Slugify(lic.RS), IsActive: true}
	req.applyTo(&org, lic)
	if err := h.DB.Create(&org).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": org})
}

// Update — PUT /api/organization (Admin): edits the partner RS profile.
func (h *OrganizationHandler) Update(c *fiber.Ctx) error {
	lic, err := activeLicense(h.DB)
	if err != nil {
		return err
	}
	org, err := partnerRS(h.DB)
	if err != nil {
		return err
	}

	var req organizationRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if err := req.validate(); err != nil {
		return err
	}

	req.applyTo(&org, lic)
	if err := h.DB.Save(&org).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": org})
}
