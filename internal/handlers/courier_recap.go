package handlers

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
)

type courierRecapRow struct {
	CourierID   uint     `json:"courier_id"`
	Name        string   `json:"name"`
	Phone       *string  `json:"phone"`
	VehicleType *string  `json:"vehicle_type"`
	IsActive    bool     `json:"is_active"`
	Total       int64    `json:"total"`
	Done        int64    `json:"done"`
	Cancelled   int64    `json:"cancelled"`
	InProgress  int64    `json:"in_progress"`
	DistanceKm  float64  `json:"distance_km"`      // total jarak order selesai
	Revenue     float64  `json:"revenue"`          // nilai order selesai
	AvgTripMin  *float64 `json:"avg_trip_minutes"` // terima paket → sampai
}

// Recap — GET /api/couriers/recap?period=daily&month=YYYY-MM | period=monthly&year=YYYY
// (daily = satu bulan, monthly = satu tahun). Per kurir: order yang
// ditugaskan dalam rentang itu (menurut tanggal ditugaskan) dan hasilnya.
// Semua kurir ikut, termasuk yang belum dapat order.
func (h *CourierHandler) Recap(c *fiber.Ctx) error {
	period, from, to, err := recapRange(c)
	if err != nil {
		return err
	}

	joinOn := "orders.courier_id = couriers.id AND COALESCE(orders.assigned_at, orders.created_at) >= ? AND COALESCE(orders.assigned_at, orders.created_at) < ?"
	metrics := fmt.Sprintf(`couriers.id AS courier_id, users.name AS name, couriers.phone, couriers.vehicle_type, couriers.is_active,
		COUNT(orders.id) AS total,
		COALESCE(SUM(orders.status = '%[1]s'), 0) AS done,
		COALESCE(SUM(orders.status = '%[2]s'), 0) AS cancelled,
		COALESCE(SUM(CASE WHEN orders.status = '%[1]s' THEN orders.distance_km ELSE 0 END), 0) AS distance_km,
		COALESCE(SUM(CASE WHEN orders.status = '%[1]s' THEN orders.price ELSE 0 END), 0) AS revenue,
		AVG(CASE WHEN orders.status = '%[1]s' AND orders.picked_up_at IS NOT NULL AND orders.delivered_at IS NOT NULL
			THEN TIMESTAMPDIFF(MINUTE, orders.picked_up_at, orders.delivered_at) END) AS avg_trip_min`,
		enums.OrderStatusDone, enums.OrderStatusCancelled)

	rows := []courierRecapRow{}
	if err := h.DB.Model(&models.Courier{}).
		Select(metrics).
		Joins("JOIN users ON users.id = couriers.user_id").
		Joins("LEFT JOIN orders ON "+joinOn, from, to).
		Group("couriers.id, users.name, couriers.phone, couriers.vehicle_type, couriers.is_active").
		Order("couriers.is_active DESC, total DESC, users.name").
		Scan(&rows).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	for i := range rows {
		rows[i].InProgress = rows[i].Total - rows[i].Done - rows[i].Cancelled
	}

	return c.JSON(fiber.Map{"data": fiber.Map{
		"period": period,
		"from":   from.Format("2006-01-02"),
		"to":     to.AddDate(0, 0, -1).Format("2006-01-02"),
		"rows":   rows,
	}})
}
