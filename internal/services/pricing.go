package services

import (
	"math"
	"sort"

	"medsend-api/internal/models"
)

// RoadFactor turns a straight-line distance into a rough road distance; only
// used when the routing server can't be reached.
const RoadFactor = 1.3

// HaversineKm is the straight-line distance between two points in km.
func HaversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLng := rad(lat2-lat1), rad(lng2-lng1)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

// EstimateKm is the fallback road distance: straight line × 1.3, rounded up to 0.1 km.
func EstimateKm(fromLat, fromLng, toLat, toLng float64) float64 {
	return math.Ceil(HaversineKm(fromLat, fromLng, toLat, toLng)*RoadFactor*10) / 10
}

// Quote prices an order of svc for a billed distance:
//   - no distance, or a service without tiers: the flat base price;
//   - otherwise the first tier whose "up to" covers the distance;
//   - beyond the last tier: its price + price_per_km for each started km over it.
func Quote(svc models.Service, km *float64) float64 {
	if km == nil || len(svc.Tiers) == 0 {
		return svc.PriceBase
	}
	tiers := append([]models.ServiceTier(nil), svc.Tiers...)
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].UpToKm < tiers[j].UpToKm })
	for _, t := range tiers {
		if *km <= t.UpToKm {
			return t.Price
		}
	}
	last := tiers[len(tiers)-1]
	if svc.PricePerKm == nil {
		return last.Price
	}
	return last.Price + math.Ceil(*km-last.UpToKm)*(*svc.PricePerKm)
}

// InRange: whether an order may go km from the RS, given its service area
// (maxKm). An unknown distance, or an RS without a limit, is always in range.
func InRange(maxKm, km *float64) bool {
	return km == nil || maxKm == nil || *km <= *maxKm
}
