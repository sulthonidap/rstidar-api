package models

import "time"

type Courier struct {
	ID             uint          `gorm:"primaryKey" json:"id"`
	UserID         uint          `gorm:"not null" json:"user_id"`
	User           *User         `json:"user,omitempty"`
	OrganizationID *uint         `json:"organization_id"`
	Organization   *Organization `json:"organization,omitempty"`
	VehicleType    *string       `json:"vehicle_type"`
	Phone          *string       `json:"phone"`
	IsActive       bool          `gorm:"default:true" json:"is_active"`
	LastLat        *float64      `gorm:"type:decimal(10,7)" json:"last_lat"`
	LastLng        *float64      `gorm:"type:decimal(10,7)" json:"last_lng"`
	LastLocationAt *time.Time    `json:"last_location_at"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

func (Courier) TableName() string { return "couriers" }
