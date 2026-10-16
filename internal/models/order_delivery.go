package models

import "time"

// ReceiverRelations: who took the package, as picked in the courier app.
var ReceiverRelations = map[string]string{
	"pasien":   "Pasien",
	"keluarga": "Keluarga",
	"tetangga": "Tetangga",
	"satpam":   "Satpam / resepsionis",
	"lainnya":  "Lainnya",
}

// OrderDelivery is the proof of delivery (POD) the courier submits when
// finishing an order: photos, who received it, and where.
type OrderDelivery struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	OrderID          uint      `gorm:"not null;uniqueIndex" json:"order_id"`
	ReceiverName     string    `gorm:"not null" json:"receiver_name"`
	ReceiverRelation string    `gorm:"type:varchar(30);not null" json:"receiver_relation"`
	Note             *string   `gorm:"type:text" json:"note"`
	Photos           []string  `gorm:"type:text;serializer:json;not null" json:"-"` // storage keys
	Thumbs           []string  `gorm:"type:text;serializer:json" json:"-"`          // square thumbnails, same order
	Lat              *float64  `gorm:"type:decimal(10,7)" json:"lat"`
	Lng              *float64  `gorm:"type:decimal(10,7)" json:"lng"`
	AccuracyM        *int      `json:"accuracy_m"`
	DeliveredBy      *uint     `json:"delivered_by"`
	DeliveredByUser  *User     `gorm:"foreignKey:DeliveredBy" json:"delivered_by_user,omitempty"`
	DeliveredAt      time.Time `json:"delivered_at"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`

	// Filled by handlers from Photos/Thumbs via the storage (not columns).
	PhotoURLs []string `gorm:"-" json:"photo_urls"`
	ThumbURLs []string `gorm:"-" json:"thumb_urls"`
}

func (OrderDelivery) TableName() string { return "order_deliveries" }
