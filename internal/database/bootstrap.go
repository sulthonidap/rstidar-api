package database

import (
	"fmt"

	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
)

var defaultRoles = []struct{ name, label string }{
	{string(enums.RoleAdmin), "Admin"},
	{string(enums.RoleOps), "Ops (Penugasan)"},
	{string(enums.RoleCco), "CCO (Kelola Order)"},
	{string(enums.RoleKurir), "Kurir"},
	{string(enums.RoleFinance), "Finance"},
	{string(enums.RoleFarmasi), "Farmasi RS"},
}

// Bootstrap makes sure every role exists. The first admin account is created
// on the "Buat Akun Admin" screen after the license is activated.
func Bootstrap(db *gorm.DB) error {
	for _, r := range defaultRoles {
		label := r.label
		role := models.Role{Name: r.name, Label: &label}
		if err := db.Where(models.Role{Name: r.name}).FirstOrCreate(&role).Error; err != nil {
			return fmt.Errorf("role %s: %w", r.name, err)
		}
	}
	return nil
}
