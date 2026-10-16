package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

type courierPeriodRecap struct {
	Done       int64   `json:"done"`
	Cancelled  int64   `json:"cancelled"`
	DistanceKm float64 `json:"distance_km"` // road distance of the delivered orders
}

type courierSelfRecap struct {
	Today     courierPeriodRecap `json:"today"`
	Month     courierPeriodRecap `json:"month"`
	Active    int64              `json:"active"`     // assigned and not finished yet
	TotalDone int64              `json:"total_done"` // all time
}

// Recap — GET /api/courier/recap: the courier's own numbers for the Profil tab.
// Delivered orders count on the day they were delivered, cancelled ones on the
// day they were cancelled (same as the Riwayat date filter).
func (h *CourierAppHandler) Recap(c *fiber.Ctx) error {
	courier := middleware.CurrentCourier(c)
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	var row struct {
		TodayDone, TodayCancelled, MonthDone, MonthCancelled, Active, TotalDone int64
		TodayKm, MonthKm                                                        float64
	}
	done, cancelled := enums.OrderStatusDone, enums.OrderStatusCancelled
	err := h.DB.Model(&models.Order{}).Where("courier_id = ?", courier.ID).Select(`
		COALESCE(SUM(CASE WHEN status = @done AND delivered_at >= @today THEN 1 ELSE 0 END), 0) AS today_done,
		COALESCE(SUM(CASE WHEN status = @cancelled AND cancelled_at >= @today THEN 1 ELSE 0 END), 0) AS today_cancelled,
		COALESCE(SUM(CASE WHEN status = @done AND delivered_at >= @today THEN distance_km ELSE 0 END), 0) AS today_km,
		COALESCE(SUM(CASE WHEN status = @done AND delivered_at >= @month THEN 1 ELSE 0 END), 0) AS month_done,
		COALESCE(SUM(CASE WHEN status = @cancelled AND cancelled_at >= @month THEN 1 ELSE 0 END), 0) AS month_cancelled,
		COALESCE(SUM(CASE WHEN status = @done AND delivered_at >= @month THEN distance_km ELSE 0 END), 0) AS month_km,
		COALESCE(SUM(CASE WHEN status IN @active THEN 1 ELSE 0 END), 0) AS active,
		COALESCE(SUM(CASE WHEN status = @done THEN 1 ELSE 0 END), 0) AS total_done`,
		map[string]any{
			"done": done, "cancelled": cancelled, "today": today, "month": month,
			"active": enums.ActiveCourierStatuses,
		}).Scan(&row).Error
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{"data": courierSelfRecap{
		Today:     courierPeriodRecap{Done: row.TodayDone, Cancelled: row.TodayCancelled, DistanceKm: row.TodayKm},
		Month:     courierPeriodRecap{Done: row.MonthDone, Cancelled: row.MonthCancelled, DistanceKm: row.MonthKm},
		Active:    row.Active,
		TotalDone: row.TotalDone,
	}})
}
