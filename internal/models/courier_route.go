package models

import "time"

// CourierRoute is the delivery order a courier plans for the packages they
// carry (saved from "Urutan antar" in the courier app), shown to Ops/Admin.
// FinishedAt nil = the courier's open route.
type CourierRoute struct {
	ID         uint               `gorm:"primaryKey" json:"id"`
	CourierID  uint               `gorm:"not null" json:"courier_id"`
	Courier    *Courier           `json:"courier,omitempty"`
	StartLat   *float64           `gorm:"type:decimal(10,7)" json:"start_lat"`
	StartLng   *float64           `gorm:"type:decimal(10,7)" json:"start_lng"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt *time.Time         `json:"finished_at"`
	Stops      []CourierRouteStop `gorm:"foreignKey:RouteID" json:"stops"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

func (CourierRoute) TableName() string { return "courier_routes" }

// CourierRouteStop is one package on a route, Seq 1 = delivered first.
type CourierRouteStop struct {
	ID      uint   `gorm:"primaryKey" json:"-"`
	RouteID uint   `gorm:"not null" json:"-"`
	OrderID uint   `gorm:"not null" json:"order_id"`
	Order   *Order `json:"order,omitempty"`
	Seq     int    `gorm:"not null" json:"seq"`
}

func (CourierRouteStop) TableName() string { return "courier_route_stops" }
