package models

import "time"

// Ward: bangsal rawat inap (master data), dipilih di order farmasi rawat inap.
type Ward struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"not null" json:"name"`
	IsActive  bool      `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Ward) TableName() string { return "wards" }
