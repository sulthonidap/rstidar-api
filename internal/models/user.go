package models

import (
	"time"

	"medsend-api/internal/enums"
)

type User struct {
	ID             uint          `gorm:"primaryKey" json:"id"`
	Name           string        `gorm:"not null" json:"name"`
	Email          string        `gorm:"uniqueIndex;not null" json:"email"`
	Phone          *string       `json:"phone"`
	Password       string        `gorm:"not null" json:"-"`
	RoleID         *uint         `json:"role_id"`
	Role           *Role         `json:"role,omitempty"`
	OrganizationID *uint         `json:"organization_id"`
	Organization   *Organization `json:"organization,omitempty"`
	IsActive       bool          `gorm:"default:true" json:"is_active"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

func (User) TableName() string { return "users" }

func (u *User) RoleName() enums.UserRole {
	if u.Role == nil {
		return ""
	}
	return enums.UserRole(u.Role.Name)
}

func (u *User) HasRole(role enums.UserRole) bool {
	return u.RoleName() == role
}

func (u *User) HasAnyRole(roles ...enums.UserRole) bool {
	for _, r := range roles {
		if u.HasRole(r) {
			return true
		}
	}
	return false
}

func (u *User) IsAdmin() bool {
	return u.HasRole(enums.RoleAdmin)
}
