package models

import (
	"time"

	"medsend-api/internal/enums"
)

type OrderStatusHistory struct {
	ID            uint               `gorm:"primaryKey" json:"id"`
	OrderID       uint               `gorm:"not null;index" json:"order_id"`
	FromStatus    *enums.OrderStatus `gorm:"type:varchar(20)" json:"from_status"`
	ToStatus      enums.OrderStatus  `gorm:"type:varchar(20);not null" json:"to_status"`
	ChangedBy     *uint              `json:"changed_by"`
	ChangedByUser *User              `gorm:"foreignKey:ChangedBy" json:"changed_by_user,omitempty"`
	Note          *string            `gorm:"type:text" json:"note"`
	ChangedAt     time.Time          `gorm:"not null" json:"changed_at"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

func (OrderStatusHistory) TableName() string { return "order_status_history" }
