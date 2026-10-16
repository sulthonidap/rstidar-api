package models

import "time"

// AppSetting is one key/value setting (e.g. "payment.bank_name").
type AppSetting struct {
	Name      string    `gorm:"primaryKey;type:varchar(64)" json:"name"`
	Value     *string   `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AppSetting) TableName() string { return "app_settings" }
