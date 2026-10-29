package handlers

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"

	"medsend-api/internal/models"
)

type returningPatientRequest struct {
	// The phone number or the medical record number used on an earlier order.
	Query string `json:"query"`
}

// ReturningPatient — POST /api/public/returning-patient {query}.
// "Pasien lama" on the public form: the latest earlier order of this RS whose
// medical record number, or phone number (any format), is the query; its
// recipient, patient and address are returned to prefill the form. One
// identifier is enough (decided 2026-10-06), so the route is rate limited
// against guessing and returns only those prefill fields.
func (h *PublicHandler) ReturningPatient(c *fiber.Ctx) error {
	var req returningPatientRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	q := strings.TrimSpace(req.Query)
	if q == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "isi No. HP atau No. Rekam Medis")
	}
	org, err := partnerRS(h.DB)
	if err != nil {
		return err
	}

	var found *models.Order
	newer := func(o models.Order) {
		if found == nil || o.ID > found.ID {
			found = &o
		}
	}

	var byRM models.Order
	if err := h.DB.Where("organization_id = ? AND medical_record_no = ?", org.ID, q).
		Order("id DESC").First(&byRM).Error; err == nil {
		newer(byRM)
	}

	// Phones are stored as typed (+62 812-…, 0812…): narrow down with the last
	// digits in SQL, then compare the normalized numbers.
	if phone := normalizePhone(q); len(phone) >= 8 {
		digitsOnly := "REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(customer_phone, ' ', ''), '-', ''), '(', ''), ')', ''), '+', '')"
		var candidates []models.Order
		h.DB.Where("organization_id = ? AND "+digitsOnly+" LIKE ?", org.ID, "%"+phone[len(phone)-8:]).
			Order("id DESC").Limit(20).Find(&candidates)
		for _, o := range candidates {
			if normalizePhone(o.CustomerPhone) == phone {
				newer(o)
				break
			}
		}
	}

	if found == nil {
		return fiber.NewError(fiber.StatusNotFound,
			"Data pasien tidak ditemukan. Periksa lagi No. HP / No. Rekam Medis, atau isi data secara manual.")
	}
	return c.JSON(fiber.Map{"data": fiber.Map{
		"customer_name":       found.CustomerName,
		"customer_phone":      found.CustomerPhone,
		"patient_name":        found.PatientName,
		"medical_record_no":   found.MedicalRecordNo,
		"dropoff_address":     found.DropoffAddress,
		"dropoff_lat":         found.DropoffLat,
		"dropoff_lng":         found.DropoffLng,
		"dropoff_region_code": found.DropoffRegionCode,
	}})
}

// clientIP: the visitor's address. Behind the reverse proxy (Traefik on
// Dokploy) every request comes from the proxy, which appends the real client
// to X-Forwarded-For — the last entry, the one a client can't forge.
func clientIP(c *fiber.Ctx) string {
	if xff := c.Get(fiber.HeaderXForwardedFor); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	return c.IP()
}

// ReturningPatientLimit: at most 10 lookups a minute per visitor.
func ReturningPatientLimit() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:          10,
		Expiration:   time.Minute,
		KeyGenerator: clientIP,
		LimitReached: func(c *fiber.Ctx) error {
			return fiber.NewError(fiber.StatusTooManyRequests, "Terlalu banyak percobaan. Coba lagi dalam 1 menit.")
		},
	})
}
