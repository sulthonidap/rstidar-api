package handlers

import (
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

type saveRouteRequest struct {
	OrderIDs []uint   `json:"order_ids"` // delivery order, first stop first
	StartLat *float64 `json:"start_lat"` // where the courier is now (optional)
	StartLng *float64 `json:"start_lng"`
}

// SaveRoute — PUT /api/courier/route {order_ids, start_lat, start_lng}: the
// delivery order from "Urutan antar", so Ops/Admin can follow it on the map.
// It updates the courier's open route (re-planning mid-trip keeps the packages
// already delivered in front); once every package on it is finished, a new
// route starts.
func (h *CourierAppHandler) SaveRoute(c *fiber.Ctx) error {
	courier := middleware.CurrentCourier(c)

	var req saveRouteRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if len(req.OrderIDs) == 0 || len(req.OrderIDs) > maxBulkOrders {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "tidak ada paket untuk rute ini")
	}
	planned := map[uint]bool{}
	for _, id := range req.OrderIDs {
		if planned[id] {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "paket yang sama muncul dua kali di rute")
		}
		planned[id] = true
	}
	var count int64
	h.myOrders(c).Where("id IN ? AND status = ?", req.OrderIDs, enums.OrderStatusOnDelivery).Count(&count)
	if int(count) != len(req.OrderIDs) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "ada paket yang tidak sedang kamu antar, muat ulang daftarnya")
	}
	// A bad GPS fix just means the route is drawn from the RS.
	if !validPoint(req.StartLat, req.StartLng) {
		req.StartLat, req.StartLng = nil, nil
	}

	now := time.Now()
	var route models.CourierRoute
	var kept []uint // finished stops of the same trip, kept up front
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		open := openRoute(tx, courier.ID)
		if open != nil && routeFinished(open) {
			if err := tx.Model(open).Update("finished_at", now).Error; err != nil {
				return err
			}
			open = nil
		}

		// Same trip: the packages already delivered (or cancelled) keep their place up front.
		if open != nil {
			route = *open
			for _, s := range open.Stops {
				if s.Order != nil && s.Order.Status.IsFinal() && !planned[s.OrderID] {
					kept = append(kept, s.OrderID)
				}
			}
			if route.StartLat == nil && req.StartLat != nil {
				route.StartLat, route.StartLng = req.StartLat, req.StartLng
			}
			if err := tx.Where("route_id = ?", route.ID).Delete(&models.CourierRouteStop{}).Error; err != nil {
				return err
			}
			if err := tx.Model(&route).Select("start_lat", "start_lng", "updated_at").Updates(&route).Error; err != nil {
				return err
			}
		} else {
			route = models.CourierRoute{CourierID: courier.ID, StartLat: req.StartLat, StartLng: req.StartLng, StartedAt: now}
			if err := tx.Create(&route).Error; err != nil {
				return err
			}
		}

		stops := make([]models.CourierRouteStop, 0, len(kept)+len(req.OrderIDs))
		for _, id := range append(kept, req.OrderIDs...) {
			stops = append(stops, models.CourierRouteStop{RouteID: route.ID, OrderID: id, Seq: len(stops) + 1})
		}
		return tx.Create(&stops).Error
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"id": route.ID, "started_at": route.StartedAt, "stops": len(kept) + len(req.OrderIDs)}})
}

// openRoute: the courier's route still in progress (with its stops and their orders), or nil.
func openRoute(db *gorm.DB, courierID uint) *models.CourierRoute {
	var route models.CourierRoute
	if db.Where("courier_id = ? AND finished_at IS NULL", courierID).
		Preload("Stops", func(q *gorm.DB) *gorm.DB { return q.Order("seq") }).Preload("Stops.Order").
		Order("id DESC").Limit(1).Find(&route).RowsAffected == 0 {
		return nil
	}
	return &route
}

// routeFinished: every package on the route has been delivered or cancelled.
func routeFinished(route *models.CourierRoute) bool {
	for _, s := range route.Stops {
		if s.Order != nil && !s.Order.Status.IsFinal() {
			return false
		}
	}
	return true
}

// closeRouteIfDone closes the courier's open route after its last package is delivered.
func closeRouteIfDone(db *gorm.DB, courierID uint) {
	if route := openRoute(db, courierID); route != nil && len(route.Stops) > 0 && routeFinished(route) {
		db.Model(route).Update("finished_at", time.Now())
	}
}

// sortStops keeps a preloaded route's stops in delivery order.
func sortStops(route *models.CourierRoute) {
	sort.Slice(route.Stops, func(i, j int) bool { return route.Stops[i].Seq < route.Stops[j].Seq })
}

// MyRoute — GET /api/courier/route: the courier's open route (order ids in
// delivery order) so the app knows which packages are covered; null when none.
func (h *CourierAppHandler) MyRoute(c *fiber.Ctx) error {
	route := openRoute(h.DB, middleware.CurrentCourier(c).ID)
	if route == nil {
		return c.JSON(fiber.Map{"data": nil})
	}
	ids := make([]uint, 0, len(route.Stops))
	for _, s := range route.Stops {
		ids = append(ids, s.OrderID)
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"id": route.ID, "updated_at": route.UpdatedAt, "order_ids": ids}})
}

// inOpenRoute: the package is on the route the courier saved (required before delivering it).
func inOpenRoute(db *gorm.DB, courierID, orderID uint) bool {
	var n int64
	db.Model(&models.CourierRouteStop{}).
		Joins("JOIN courier_routes ON courier_routes.id = courier_route_stops.route_id").
		Where("courier_routes.courier_id = ? AND courier_routes.finished_at IS NULL AND courier_route_stops.order_id = ?", courierID, orderID).
		Count(&n)
	return n > 0
}
