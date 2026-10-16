package models

import "time"

// ServiceTier: orders of a service billed up to UpToKm cost Price.
type ServiceTier struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ServiceID uint      `gorm:"not null" json:"service_id"`
	UpToKm    float64   `gorm:"type:decimal(6,2);not null" json:"up_to_km"`
	Price     float64   `gorm:"type:decimal(12,2);not null" json:"price"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ServiceTier) TableName() string { return "service_tiers" }
