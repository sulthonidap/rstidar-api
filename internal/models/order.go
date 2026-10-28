package models

import (
	"time"

	"medsend-api/internal/enums"
)

type Order struct {
	ID             uint          `gorm:"primaryKey" json:"id"`
	Code           string        `gorm:"uniqueIndex;not null" json:"code"`
	OrganizationID *uint         `json:"organization_id"`
	Organization   *Organization `json:"organization,omitempty"`
	CustomerID     *uint         `json:"customer_id"`

	CustomerName  string `gorm:"not null" json:"customer_name"`
	CustomerPhone string `gorm:"not null" json:"customer_phone"`
	// Optional patient data (internal only, never on the public tracking page).
	PatientName     *string `json:"patient_name"`
	MedicalRecordNo *string `json:"medical_record_no"`
	// Program ongkir gratis (lihat handlers/free_program.go): kategori pasien +
	// domisili yang dicek petugas. Gratis → Price 0, tarif normal di NormalPrice.
	PatientCategory enums.PatientCategory `gorm:"type:varchar(20);not null;default:umum" json:"patient_category"`
	DomicileChecked bool                  `gorm:"not null;default:false" json:"domicile_checked"`
	IsFree          bool                  `gorm:"not null;default:false" json:"is_free"`
	NormalPrice     *float64              `gorm:"type:decimal(12,2)" json:"normal_price"`
	FreeVerifiedBy  *uint                 `json:"free_verified_by"`
	FreeVerifiedAt  *time.Time            `json:"free_verified_at"`
	// Farmasi: rawat jalan (+ no. antrian farmasi) atau rawat inap (+ bangsal).
	CareType        *enums.CareType `gorm:"type:varchar(20)" json:"care_type"`
	PharmacyQueueNo *string         `gorm:"type:varchar(20)" json:"pharmacy_queue_no"`
	WardID          *uint           `json:"ward_id"`
	Ward            *Ward           `json:"ward,omitempty"`
	PickupAddress   string          `gorm:"type:text;not null" json:"pickup_address"`
	DropoffAddress  string          `gorm:"type:text;not null" json:"dropoff_address"`
	PickupLat       *float64        `gorm:"type:decimal(10,7)" json:"pickup_lat"`
	PickupLng       *float64        `gorm:"type:decimal(10,7)" json:"pickup_lng"`
	DropoffLat      *float64        `gorm:"type:decimal(10,7)" json:"dropoff_lat"`
	DropoffLng      *float64        `gorm:"type:decimal(10,7)" json:"dropoff_lng"`

	ServiceID *uint    `json:"service_id"`
	Service   *Service `json:"service,omitempty"`
	CourierID *uint    `json:"courier_id"`
	Courier   *Courier `json:"courier,omitempty"`

	Status enums.OrderStatus `gorm:"type:varchar(20);not null;default:pending;index" json:"status"`
	Source enums.OrderSource `gorm:"type:varchar(20);not null;default:public" json:"source"`

	CreatedBy *uint `json:"created_by"`
	Creator   *User `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`

	Price float64 `gorm:"type:decimal(12,2);not null;default:0" json:"price"`
	// Distance RS → destination in km (nil = destination point unknown), and
	// where it came from: "road" (routing server) or "estimate" (straight line × 1.3).
	DistanceKm     *float64 `gorm:"type:decimal(8,2)" json:"distance_km"`
	DistanceSource *string  `gorm:"type:varchar(10)" json:"distance_source"`
	Notes          *string  `gorm:"type:text" json:"notes"`

	// Delivery schedule; nil date = as soon as possible.
	ScheduledDate *Date               `gorm:"type:date" json:"scheduled_date"`
	ScheduledSlot *enums.ScheduleSlot `gorm:"type:varchar(10)" json:"scheduled_slot"`

	ConfirmedAt *time.Time `json:"confirmed_at"`
	ReadyAt     *time.Time `json:"ready_at"`
	CollectedAt *time.Time `json:"collected_at"`
	PackedAt    *time.Time `json:"packed_at"`
	AssignedAt  *time.Time `json:"assigned_at"`
	PickedUpAt  *time.Time `json:"picked_up_at"`
	DeliveredAt *time.Time `json:"delivered_at"`
	CancelledAt *time.Time `json:"cancelled_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	StatusHistory []OrderStatusHistory `gorm:"foreignKey:OrderID" json:"status_history,omitempty"`
	Transaction   *Transaction         `gorm:"foreignKey:OrderID" json:"transaction,omitempty"`
	Delivery      *OrderDelivery       `gorm:"foreignKey:OrderID" json:"delivery,omitempty"`
}

func (Order) TableName() string { return "orders" }
