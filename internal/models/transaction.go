package models

import (
	"time"

	"medsend-api/internal/enums"
)

type Transaction struct {
	ID              uint                 `gorm:"primaryKey" json:"id"`
	OrderID         uint                 `gorm:"not null;index" json:"order_id"`
	Order           *Order               `json:"order,omitempty"`
	OrganizationID  *uint                `json:"organization_id"`
	Organization    *Organization        `json:"organization,omitempty"`
	Amount          float64              `gorm:"type:decimal(12,2);not null;default:0" json:"amount"`
	PaymentMethod   *enums.PaymentMethod `gorm:"type:varchar(10)" json:"payment_method"` // nil for orders from before payment methods
	PaymentStatus   enums.PaymentStatus  `gorm:"type:varchar(20);not null;default:unpaid" json:"payment_status"`
	Proof           *string              `json:"-"` // storage key of the transfer/QRIS receipt
	ProofURL        *string              `gorm:"-" json:"proof_url"`
	ProofUploadedAt *time.Time           `json:"proof_uploaded_at"`
	Note            *string              `json:"note"` // e.g. why a proof was rejected
	RecordedBy      *uint                `json:"recorded_by"`
	RecordedByUser  *User                `gorm:"foreignKey:RecordedBy" json:"recorded_by_user,omitempty"`
	PaidAt          *time.Time           `json:"paid_at"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

func (Transaction) TableName() string { return "transactions" }
