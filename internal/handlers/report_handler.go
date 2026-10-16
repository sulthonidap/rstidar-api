package handlers

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

// ReportHandler — rekap order harian/bulanan.
type ReportHandler struct {
	DB *gorm.DB
}

type recapRow struct {
	Period      string         `json:"period"` // "2026-09-29" (harian) or "2026-09" (bulanan)
	Total       int64          `json:"total"`
	Done        int64          `json:"done"`
	Cancelled   int64          `json:"cancelled"`
	InProgress  int64          `json:"in_progress"`
	Revenue     *float64       `json:"revenue,omitempty"` // Admin & Finance only (nil = hidden)
	AvgReadyMin *float64       `json:"avg_ready_minutes"` // masuk → siap diambil (not for farmasi orders, they start ready)
	AvgDoneMin  *float64       `json:"avg_done_minutes"`  // masuk → selesai
	ByService   map[uint]int64 `json:"by_service" gorm:"-"`
}

type recapService struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type recapResult struct {
	Period   string         `json:"period"` // "daily" / "monthly"
	From     string         `json:"from"`
	To       string         `json:"to"`
	Rows     []recapRow     `json:"rows"`
	Total    recapRow       `json:"total"`
	Services []recapService `json:"services"`
}

// recapRange reads ?period=daily&month=YYYY-MM or ?period=monthly&year=YYYY.
func recapRange(c *fiber.Ctx) (period string, from, to time.Time, err error) {
	now := time.Now()
	period = c.Query("period", "daily")
	switch period {
	case "daily":
		month := c.Query("month", now.Format("2006-01"))
		from, err = time.ParseInLocation("2006-01", month, time.Local)
		if err != nil {
			return "", from, to, fiber.NewError(fiber.StatusBadRequest, "month harus YYYY-MM")
		}
		return period, from, from.AddDate(0, 1, 0), nil
	case "monthly":
		year := c.Query("year", now.Format("2006"))
		from, err = time.ParseInLocation("2006", year, time.Local)
		if err != nil {
			return "", from, to, fiber.NewError(fiber.StatusBadRequest, "year harus YYYY")
		}
		return period, from, from.AddDate(1, 0, 0), nil
	}
	return "", from, to, fiber.NewError(fiber.StatusBadRequest, "period harus daily atau monthly")
}

// recap counts the orders that came in within the range (by created date),
// per day or month: how many finished, were cancelled or are still in progress,
// the revenue of the finished ones, average durations, and counts per service.
func (h *ReportHandler) recap(c *fiber.Ctx) (*recapResult, error) {
	// Money is for Admin & Finance only; the other staff see the counts.
	withRevenue := middleware.CurrentUser(c).HasAnyRole(enums.RoleAdmin, enums.RoleFinance)
	period, from, to, err := recapRange(c)
	if err != nil {
		return nil, err
	}

	base := h.DB.Model(&models.Order{}).
		Where("orders.created_at >= ? AND orders.created_at < ?", from, to)
	base = base.Session(&gorm.Session{})

	bucketFmt, layout := "%Y-%m-%d", "2006-01-02"
	if period == "monthly" {
		bucketFmt, layout = "%Y-%m", "2006-01"
	}
	metrics := fmt.Sprintf(`COUNT(*) AS total,
		COALESCE(SUM(status = '%[1]s'), 0) AS done,
		COALESCE(SUM(status = '%[2]s'), 0) AS cancelled,
		COALESCE(SUM(CASE WHEN status = '%[1]s' THEN price ELSE 0 END), 0) AS revenue,
		AVG(CASE WHEN ready_at IS NOT NULL AND source <> '%[3]s' THEN TIMESTAMPDIFF(MINUTE, created_at, ready_at) END) AS avg_ready_min,
		AVG(CASE WHEN status = '%[1]s' AND delivered_at IS NOT NULL THEN TIMESTAMPDIFF(MINUTE, created_at, delivered_at) END) AS avg_done_min`,
		enums.OrderStatusDone, enums.OrderStatusCancelled, enums.OrderSourceFarmasi)

	var grouped []recapRow
	if err := base.Select(fmt.Sprintf("DATE_FORMAT(created_at, '%s') AS period, %s", bucketFmt, metrics)).
		Group("period").Scan(&grouped).Error; err != nil {
		return nil, fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	var total recapRow
	if err := base.Select(metrics).Scan(&total).Error; err != nil {
		return nil, fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	var perService []struct {
		Period    string
		ServiceID *uint
		Count     int64
	}
	if err := base.Select(fmt.Sprintf("DATE_FORMAT(created_at, '%s') AS period, service_id, COUNT(*) AS count", bucketFmt)).
		Group("period, service_id").Scan(&perService).Error; err != nil {
		return nil, fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	byPeriod := map[string]*recapRow{}
	for i := range grouped {
		byPeriod[grouped[i].Period] = &grouped[i]
	}
	total.ByService = map[uint]int64{}
	serviceIDs := []uint{}
	for _, s := range perService {
		if s.ServiceID == nil {
			continue
		}
		row := byPeriod[s.Period]
		if row.ByService == nil {
			row.ByService = map[uint]int64{}
		}
		row.ByService[*s.ServiceID] += s.Count
		if _, seen := total.ByService[*s.ServiceID]; !seen {
			serviceIDs = append(serviceIDs, *s.ServiceID)
		}
		total.ByService[*s.ServiceID] += s.Count
	}
	services := []recapService{}
	if len(serviceIDs) > 0 {
		if err := h.DB.Model(&models.Service{}).Where("id IN ?", serviceIDs).Order("price_base, id").
			Select("id, name").Scan(&services).Error; err != nil {
			return nil, fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
	}

	// Every day/month in the range, including empty ones.
	rows := []recapRow{}
	for t := from; t.Before(to); {
		key := t.Format(layout)
		row := recapRow{Period: key, ByService: map[uint]int64{}}
		if r, ok := byPeriod[key]; ok {
			row = *r
			if row.ByService == nil {
				row.ByService = map[uint]int64{}
			}
		}
		row.InProgress = row.Total - row.Done - row.Cancelled
		rows = append(rows, row)
		if period == "monthly" {
			t = t.AddDate(0, 1, 0)
		} else {
			t = t.AddDate(0, 0, 1)
		}
	}
	total.Period = "total"
	total.InProgress = total.Total - total.Done - total.Cancelled
	for i := range rows {
		rows[i].Revenue = revenueFor(rows[i].Revenue, withRevenue)
	}
	total.Revenue = revenueFor(total.Revenue, withRevenue)

	return &recapResult{
		Period: period, From: from.Format("2006-01-02"), To: to.AddDate(0, 0, -1).Format("2006-01-02"),
		Rows: rows, Total: total, Services: services,
	}, nil
}

// Recap — GET /api/reports/recap?period=daily&month=YYYY-MM | period=monthly&year=YYYY
func (h *ReportHandler) Recap(c *fiber.Ctx) error {
	res, err := h.recap(c)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": res})
}

// revenueFor: the row's revenue for whoever may see it (0 for an empty period), nil otherwise.
func revenueFor(v *float64, show bool) *float64 {
	if !show {
		return nil
	}
	if v == nil {
		zero := 0.0
		return &zero
	}
	return v
}
