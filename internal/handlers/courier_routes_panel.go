package handlers

import (
	"math"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
	"medsend-api/internal/routing"
	"medsend-api/internal/services"
)

// CourierRoutesHandler — "Rute kurir" on the panel map (Admin & Ops).
type CourierRoutesHandler struct {
	DB     *gorm.DB
	Router routing.Router
}

type routeStopView struct {
	Seq            int               `json:"seq"`
	OrderID        uint              `json:"order_id"`
	Code           string            `json:"code"`
	CustomerName   string            `json:"customer_name"`
	DropoffAddress string            `json:"dropoff_address"`
	Lat            *float64          `json:"lat"` // destination on the map (nil = not placed)
	Lng            *float64          `json:"lng"`
	Status         enums.OrderStatus `json:"status"`
	// Realisation: where and when the courier actually handed it over (proof of delivery GPS).
	DeliveredAt  *time.Time `json:"delivered_at"`
	DeliveredLat *float64   `json:"delivered_lat"`
	DeliveredLng *float64   `json:"delivered_lng"`
	OffByM       *int       `json:"off_by_m"` // handover point ↔ destination, metres
}

type routeView struct {
	ID         uint            `json:"id"`
	CourierID  uint            `json:"courier_id"`
	Courier    string          `json:"courier"`
	Vehicle    *string         `json:"vehicle"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	Start      *[2]float64     `json:"start"`      // first point of the line
	StartFrom  string          `json:"start_from"` // "gps" (courier's position) | "rs"
	Stops      []routeStopView `json:"stops"`
	Done       int             `json:"done"`
	Km         float64         `json:"km"`
	Path       [][2]float64    `json:"path"`        // planned line through the stops, in order
	PathSource string          `json:"path_source"` // "road" | "estimate" (straight segments)
}

// Index — GET /api/couriers/routes?date=YYYY-MM-DD (default today): the routes
// that were running that day, with the planned line (by road when the routing
// server answers) and where each package was actually handed over.
func (h *CourierRoutesHandler) Index(c *fiber.Ctx) error {
	day := time.Now()
	if raw := c.Query("date"); raw != "" {
		d, err := time.ParseInLocation("2006-01-02", raw, time.Local)
		if err != nil {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "tanggal tidak valid (YYYY-MM-DD)")
		}
		day = d
	}
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 0, 1)

	var routes []models.CourierRoute
	err := h.DB.Where("started_at < ? AND (finished_at IS NULL OR finished_at >= ?)", to, from).
		Preload("Courier.User").
		Preload("Stops", func(q *gorm.DB) *gorm.DB { return q.Order("seq") }).
		Preload("Stops.Order.Delivery").
		Order("started_at").Find(&routes).Error
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	var rs *models.Organization
	if org, err := partnerRS(h.DB); err == nil {
		rs = &org
	}
	views := make([]routeView, 0, len(routes))
	for i := range routes {
		views = append(views, h.view(c, &routes[i], rs))
	}
	return c.JSON(fiber.Map{"data": fiber.Map{"date": from.Format("2006-01-02"), "routes": views}})
}

func (h *CourierRoutesHandler) view(c *fiber.Ctx, r *models.CourierRoute, org *models.Organization) routeView {
	sortStops(r)
	v := routeView{ID: r.ID, CourierID: r.CourierID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Stops: []routeStopView{}}
	if r.Courier != nil {
		v.Vehicle = r.Courier.VehicleType
		if r.Courier.User != nil {
			v.Courier = r.Courier.User.Name
		}
	}
	switch {
	case r.StartLat != nil && r.StartLng != nil:
		v.Start, v.StartFrom = &[2]float64{*r.StartLat, *r.StartLng}, "gps"
	case org != nil && org.Lat != nil && org.Lng != nil:
		v.Start, v.StartFrom = &[2]float64{*org.Lat, *org.Lng}, "rs"
	}

	points := [][2]float64{}
	if v.Start != nil {
		points = append(points, *v.Start)
	}
	for _, s := range r.Stops {
		o := s.Order
		if o == nil {
			continue
		}
		sv := routeStopView{Seq: s.Seq, OrderID: o.ID, Code: o.Code, CustomerName: o.CustomerName,
			DropoffAddress: o.DropoffAddress, Lat: o.DropoffLat, Lng: o.DropoffLng, Status: o.Status}
		if o.Status == enums.OrderStatusDone {
			v.Done++
		}
		if d := o.Delivery; d != nil {
			at := d.DeliveredAt
			sv.DeliveredAt, sv.DeliveredLat, sv.DeliveredLng = &at, d.Lat, d.Lng
			if d.Lat != nil && d.Lng != nil && o.DropoffLat != nil && o.DropoffLng != nil {
				m := int(math.Round(services.HaversineKm(*d.Lat, *d.Lng, *o.DropoffLat, *o.DropoffLng) * 1000))
				sv.OffByM = &m
			}
		}
		if o.DropoffLat != nil && o.DropoffLng != nil {
			points = append(points, [2]float64{*o.DropoffLat, *o.DropoffLng})
		}
		v.Stops = append(v.Stops, sv)
	}

	// The planned line, leg by leg (cached by the router, so redrawing is cheap).
	v.Path, v.PathSource = [][2]float64{}, distanceRoad
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		km, source, path := measure(c.UserContext(), h.Router, a[0], a[1], b[0], b[1])
		v.Km += km
		if source != distanceRoad || len(path) == 0 {
			v.PathSource = distanceEstimate
			path = [][2]float64{a, b}
		}
		v.Path = append(v.Path, path...)
	}
	v.Km = math.Round(v.Km*10) / 10
	return v
}
