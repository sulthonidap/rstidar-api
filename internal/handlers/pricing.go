package handlers

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/models"
	"medsend-api/internal/routing"
	"medsend-api/internal/services"
)

const (
	distanceRoad     = "road"
	distanceEstimate = "estimate"
)

// measure: road distance (and the route's shape) between two points from the
// routing server, or the straight-line × 1.3 estimate (no shape) when there's
// no server or it doesn't answer.
func measure(ctx context.Context, router routing.Router, fromLat, fromLng, toLat, toLng float64) (float64, string, [][2]float64) {
	if router != nil {
		r, err := router.Route(ctx, fromLat, fromLng, toLat, toLng)
		if err == nil {
			return r.Km, distanceRoad, r.Path
		}
		log.Printf("routing: %v (pakai perkiraan garis lurus × 1,3)", err)
	}
	return services.EstimateKm(fromLat, fromLng, toLat, toLng), distanceEstimate, nil
}

// measureOrder sets the order's distance (RS pickup point → destination, by
// road); nil without both points.
func measureOrder(ctx context.Context, db *gorm.DB, router routing.Router, order *models.Order) {
	pickupLat, pickupLng := order.PickupLat, order.PickupLng
	if pickupLat == nil || pickupLng == nil {
		pickupLat, pickupLng = rsPoint(db, order.OrganizationID)
	}
	order.DistanceKm, order.DistanceSource = nil, nil
	if pickupLat != nil && pickupLng != nil && order.DropoffLat != nil && order.DropoffLng != nil {
		km, source, _ := measure(ctx, router, *pickupLat, *pickupLng, *order.DropoffLat, *order.DropoffLng)
		order.DistanceKm, order.DistanceSource = &km, &source
	}
}

// applyPrice measures the order's distance and prices it from the service's
// distance tiers. Without a destination point, or for a service without tiers,
// the price is the flat base price. Refuses (422) a destination outside the
// RS's service area. service must be loaded with its Tiers.
func applyPrice(ctx context.Context, db *gorm.DB, router routing.Router, service models.Service, order *models.Order) error {
	measureOrder(ctx, db, router, order)
	if order.DistanceKm != nil && order.OrganizationID != nil {
		var org models.Organization
		if db.Select("name, max_distance_km").First(&org, *order.OrganizationID).Error == nil {
			if err := checkRange(org, order.DistanceKm); err != nil {
				return err
			}
		}
	}
	order.Price = services.Quote(service, order.DistanceKm)
	return nil
}

// checkRange refuses (422) a distance beyond the RS's service area:
// "Jarak 42,3 km di luar wilayah layanan RS A (maks. 25 km)".
func checkRange(org models.Organization, km *float64) error {
	if services.InRange(org.MaxDistanceKm, km) {
		return nil
	}
	return fiber.NewError(fiber.StatusUnprocessableEntity, fmt.Sprintf(
		"Jarak %s km di luar wilayah layanan %s (maks. %s km)",
		formatKm(*km), org.Name, formatKm(*org.MaxDistanceKm)))
}

// formatKm: 42.3 → "42,3", 30 → "30".
func formatKm(km float64) string {
	return strings.Replace(strconv.FormatFloat(km, 'f', -1, 64), ".", ",", 1)
}

// withTiers loads a service for pricing.
func withTiers(db *gorm.DB) *gorm.DB { return db.Preload("Tiers") }

// quotePrice: a service's price for an already measured distance.
func quotePrice(service models.Service, km *float64) float64 {
	return services.Quote(service, km)
}
