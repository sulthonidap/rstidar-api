package models

import "time"

// Customer is a saved recipient of an RS, so staff can reuse their details for
// the next internal order. Orders keep their own copy of name/phone/address
// (a customer edit never rewrites order history).
type Customer struct {
	ID             uint          `gorm:"primaryKey" json:"id"`
	OrganizationID uint          `gorm:"not null" json:"organization_id"`
	Organization   *Organization `json:"organization,omitempty"`
	Name           string        `gorm:"not null" json:"name"`
	Phone          string        `gorm:"not null" json:"phone"`
	Address        string        `gorm:"type:text;not null" json:"address"`
	RegionCode     *string       `gorm:"type:varchar(13)" json:"region_code"` // kelurahan/desa (kode Kemendagri)
	Lat            *float64      `gorm:"type:decimal(10,7)" json:"lat"`       // map point of the address, if known
	Lng            *float64      `gorm:"type:decimal(10,7)" json:"lng"`
	Notes          *string       `gorm:"type:text" json:"notes"`
	CreatedBy      *uint         `json:"created_by"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`

	// Filled by the list endpoint only.
	OrderCount  int64      `gorm:"-:migration;->" json:"order_count"`
	LastOrderAt *time.Time `gorm:"-:migration;->" json:"last_order_at"`
}

func (Customer) TableName() string { return "customers" }
