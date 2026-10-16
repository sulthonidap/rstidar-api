package handlers

import (
	"github.com/gofiber/fiber/v2"

	"medsend-api/internal/models"
	"medsend-api/internal/services"
)

type quoteRequest struct {
	ServiceID  uint     `json:"service_id"` // 0 = every active service
	DropoffLat *float64 `json:"dropoff_lat"`
	DropoffLng *float64 `json:"dropoff_lng"`
	// Pickup point, when it isn't the RS point (an order whose pickup was moved).
	PickupLat *float64 `json:"pickup_lat"`
	PickupLng *float64 `json:"pickup_lng"`
}

type serviceQuote struct {
	ServiceID uint    `json:"service_id"`
	Price     float64 `json:"price"`
	Tiered    bool    `json:"tiered"`
}

// Quote — POST /api/public/quote: what an order would cost (and the road
// distance RS → destination), so the forms can show it before submitting.
// Same calculation as when the order is saved. With service_id: that service
// (price, tiered at the top level); without: every active service in "services".
func (h *PublicHandler) Quote(c *fiber.Ctx) error {
	var req quoteRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if !validPoint(req.DropoffLat, req.DropoffLng) || !validPoint(req.PickupLat, req.PickupLng) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "titik tujuan harus di wilayah Indonesia")
	}
	org, err := partnerRS(h.DB)
	if err != nil {
		return err
	}

	var list []models.Service
	sq := withTiers(h.DB).Where("is_active = ?", true).Order("price_base")
	if req.ServiceID != 0 {
		sq = sq.Where("id = ?", req.ServiceID)
	}
	if err := sq.Find(&list).Error; err != nil || len(list) == 0 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "layanan tidak aktif")
	}

	// The distance is the same for every service: measure it once.
	order := models.Order{OrganizationID: &org.ID, PickupLat: org.Lat, PickupLng: org.Lng,
		DropoffLat: req.DropoffLat, DropoffLng: req.DropoffLng}
	if req.PickupLat != nil {
		order.PickupLat, order.PickupLng = req.PickupLat, req.PickupLng
	}
	measureOrder(c.UserContext(), h.DB, h.Router, &order)
	quotes := make([]serviceQuote, 0, len(list))
	for _, s := range list {
		o := order
		o.Price = quotePrice(s, order.DistanceKm)
		quotes = append(quotes, serviceQuote{ServiceID: s.ID, Price: o.Price, Tiered: len(s.Tiers) > 0})
	}

	// The road's shape for the map (cached from the measurement above); empty when estimated.
	route := [][2]float64{}
	if order.DistanceSource != nil && *order.DistanceSource == distanceRoad {
		_, _, route = measure(c.UserContext(), h.Router, *order.PickupLat, *order.PickupLng, *order.DropoffLat, *order.DropoffLng)
	}

	return c.JSON(fiber.Map{"data": fiber.Map{
		"route":  route,
		"price":  quotes[0].Price,
		"tiered": quotes[0].Tiered,
		// Outside the RS's service area: an order would be refused (whatever the service).
		"out_of_range":    !services.InRange(org.MaxDistanceKm, order.DistanceKm),
		"max_distance_km": org.MaxDistanceKm,
		"distance_km":     order.DistanceKm,
		"distance_source": order.DistanceSource,
		"services":        quotes,
	}})
}
