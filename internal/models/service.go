package models

import "time"

type Service struct {
	ID          uint    `gorm:"primaryKey" json:"id"`
	Name        string  `gorm:"not null" json:"name"`
	Description *string `json:"description"`
	PriceBase   float64 `gorm:"type:decimal(12,2);default:0" json:"price_base"` // flat price, and the price when the distance is unknown
	// Distance tiers (ascending) and the charge per km beyond the last one.
	Tiers      []ServiceTier `gorm:"foreignKey:ServiceID" json:"tiers"`
	PricePerKm *float64      `gorm:"type:decimal(12,2)" json:"price_per_km"`
	IsActive   bool          `gorm:"default:true" json:"is_active"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

func (Service) TableName() string { return "services" }
