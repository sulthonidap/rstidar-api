package handlers

import (
	"fmt"
	"sort"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/models"
)

type ServiceHandler struct {
	DB *gorm.DB
}

const maxTiers = 10

func (h *ServiceHandler) Index(c *fiber.Ctx) error {
	var list []models.Service
	if err := withTiers(h.DB).Order("price_base").Find(&list).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": list})
}

type tierRequest struct {
	UpToKm float64 `json:"up_to_km"`
	Price  float64 `json:"price"`
}

type serviceRequest struct {
	Name        string   `json:"name" validate:"required"`
	Description *string  `json:"description"`
	PriceBase   *float64 `json:"price_base"`
	IsActive    *bool    `json:"is_active"`
	// Distance pricing: tiers "up to X km → price" (empty = flat price_base)
	// and the charge per km beyond the last tier (null = stay at the last price).
	Tiers      []tierRequest `json:"tiers"`
	PricePerKm *float64      `json:"price_per_km"`
}

// tiers validates and sorts the requested tiers.
func (r *serviceRequest) tiers() ([]models.ServiceTier, error) {
	if len(r.Tiers) > maxTiers {
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity, fmt.Sprintf("maksimal %d tier jarak", maxTiers))
	}
	sorted := append([]tierRequest(nil), r.Tiers...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].UpToKm < sorted[j].UpToKm })
	out := make([]models.ServiceTier, 0, len(sorted))
	for i, t := range sorted {
		if t.UpToKm <= 0 || t.UpToKm > 1000 {
			return nil, fiber.NewError(fiber.StatusUnprocessableEntity, "batas km tiap tier harus antara 0 dan 1000")
		}
		if t.Price < 0 {
			return nil, fiber.NewError(fiber.StatusUnprocessableEntity, "harga tier tidak boleh negatif")
		}
		if i > 0 && t.UpToKm == sorted[i-1].UpToKm {
			return nil, fiber.NewError(fiber.StatusUnprocessableEntity, fmt.Sprintf("batas %.1f km dipakai dua kali", t.UpToKm))
		}
		out = append(out, models.ServiceTier{UpToKm: t.UpToKm, Price: t.Price})
	}
	if r.PricePerKm != nil && *r.PricePerKm < 0 {
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity, "tarif per km tidak boleh negatif")
	}
	return out, nil
}

// saveWithTiers stores the service and replaces its tiers in one transaction.
func (h *ServiceHandler) saveWithTiers(svc *models.Service, tiers []models.ServiceTier) error {
	return h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Tiers").Save(svc).Error; err != nil {
			return err
		}
		if err := tx.Where("service_id = ?", svc.ID).Delete(&models.ServiceTier{}).Error; err != nil {
			return err
		}
		for i := range tiers {
			tiers[i].ServiceID = svc.ID
		}
		if len(tiers) > 0 {
			return tx.Create(&tiers).Error
		}
		return nil
	})
}

func (h *ServiceHandler) Store(c *fiber.Ctx) error {
	var req serviceRequest
	if err := c.BodyParser(&req); err != nil || req.Name == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "nama wajib diisi")
	}
	tiers, err := req.tiers()
	if err != nil {
		return err
	}

	svc := models.Service{Name: req.Name, Description: req.Description, IsActive: true, PricePerKm: req.PricePerKm}
	if req.PriceBase != nil {
		svc.PriceBase = *req.PriceBase
	}
	if req.IsActive != nil {
		svc.IsActive = *req.IsActive
	}

	if err := h.saveWithTiers(&svc, tiers); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	withTiers(h.DB).First(&svc, svc.ID)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": svc})
}

func (h *ServiceHandler) Update(c *fiber.Ctx) error {
	var svc models.Service
	if err := h.DB.First(&svc, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "layanan tidak ditemukan")
	}

	var req serviceRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	tiers, err := req.tiers()
	if err != nil {
		return err
	}

	if req.Name != "" {
		svc.Name = req.Name
	}
	svc.Description = req.Description
	if req.PriceBase != nil {
		svc.PriceBase = *req.PriceBase
	}
	if req.IsActive != nil {
		svc.IsActive = *req.IsActive
	}
	svc.PricePerKm = req.PricePerKm

	if err := h.saveWithTiers(&svc, tiers); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	withTiers(h.DB).First(&svc, svc.ID)
	return c.JSON(fiber.Map{"data": svc})
}
