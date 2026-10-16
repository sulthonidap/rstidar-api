package models

import "time"

type Organization struct {
	ID      uint    `gorm:"primaryKey" json:"id"`
	Name    string  `gorm:"not null" json:"name"`
	Slug    string  `gorm:"uniqueIndex;not null" json:"slug"`
	Address *string `json:"address"`
	// Map point of the RS (pickup), set by the admin; nil = not set yet.
	Lat *float64 `gorm:"type:decimal(10,7)" json:"lat"`
	Lng *float64 `gorm:"type:decimal(10,7)" json:"lng"`
	// Service area: the farthest an order may go from the RS (by road); nil = no limit.
	MaxDistanceKm *float64  `gorm:"type:decimal(6,2)" json:"max_distance_km"`
	Phone         *string   `json:"phone"`
	IsActive      bool      `gorm:"default:true" json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (Organization) TableName() string { return "organizations" }
