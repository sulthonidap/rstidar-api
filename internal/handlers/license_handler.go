package handlers

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"medsend-api/internal/enums"
	"medsend-api/internal/license"
	"medsend-api/internal/models"
)

// First run of a fresh install, before anyone can log in:
//  1. Aktivasi Lisensi — paste the license key (POST /api/public/activate);
//  2. Buat Akun Admin — the first admin account (POST /api/public/setup-admin);
//  3. after login, Setup Profil RS (POST /api/organization), named after the license.
// GET /api/public/setup tells the login page which step is next.

const settingLicense = "license.key"

var errNotActivated = fiber.NewError(fiber.StatusForbidden, "Aplikasi belum diaktivasi. Masukkan kode lisensi dulu.")

// activeLicense is the stored, verified license; errNotActivated without one.
func activeLicense(db *gorm.DB) (license.Info, error) {
	var row models.AppSetting
	if err := db.First(&row, "name = ?", settingLicense).Error; err != nil || row.Value == nil {
		return license.Info{}, errNotActivated
	}
	info, err := license.Parse(*row.Value)
	if err != nil {
		return license.Info{}, errNotActivated
	}
	return info, nil
}

// saveLicense stores the key; an existing RS profile takes the license's name.
func saveLicense(db *gorm.DB, key string, info license.Info) error {
	key = strings.TrimSpace(key)
	return db.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.OnConflict{UpdateAll: true}).
			Create(&models.AppSetting{Name: settingLicense, Value: &key}).Error
		if err != nil {
			return err
		}
		if org, err := partnerRS(tx); err == nil {
			return tx.Model(&org).Update("name", info.RS).Error
		}
		return nil
	})
}

func licenseJSON(info license.Info) fiber.Map {
	return fiber.Map{
		"no":      info.No,
		"rs":      info.RS,
		"issued":  info.Issued,
		"expires": info.Expires,
		"expired": info.Expired(time.Now()),
	}
}

type LicenseHandler struct {
	DB *gorm.DB
}

func (h *LicenseHandler) hasAdmin() bool {
	var n int64
	h.DB.Model(&models.User{}).Joins("JOIN roles ON roles.id = users.role_id").
		Where("roles.name = ?", enums.RoleAdmin).Count(&n)
	return n > 0
}

// Status — GET /api/public/setup: which first-run step is next, and the
// license (shown on the login page and in the panel).
func (h *LicenseHandler) Status(c *fiber.Ctx) error {
	info, err := activeLicense(h.DB)
	if err != nil {
		return c.JSON(fiber.Map{"data": fiber.Map{"licensed": false, "license": nil, "has_admin": false, "has_profile": false}})
	}
	_, orgErr := partnerRS(h.DB)
	return c.JSON(fiber.Map{"data": fiber.Map{
		"licensed":    true,
		"license":     licenseJSON(info),
		"has_admin":   h.hasAdmin(),
		"has_profile": orgErr == nil,
	}})
}

type licenseRequest struct {
	Key string `json:"key"`
}

// Activate — POST /api/public/activate: step 1, stores the license (once).
func (h *LicenseHandler) Activate(c *fiber.Ctx) error {
	if _, err := activeLicense(h.DB); err == nil {
		return fiber.NewError(fiber.StatusConflict, "aplikasi sudah diaktivasi")
	}
	var req licenseRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	info, err := license.Parse(req.Key)
	if err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "Kode lisensi tidak valid. Periksa lagi kodenya.")
	}
	if err := saveLicense(h.DB, req.Key, info); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": licenseJSON(info)})
}

type setupAdminRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// SetupAdmin — POST /api/public/setup-admin: step 2, the first admin account.
// Refused once any admin exists (more users are added from Kelola User).
func (h *LicenseHandler) SetupAdmin(c *fiber.Ctx) error {
	if _, err := activeLicense(h.DB); err != nil {
		return err
	}
	if h.hasAdmin() {
		return fiber.NewError(fiber.StatusConflict, "akun admin sudah ada, silakan login")
	}
	var req setupAdminRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	req.Name, req.Email = strings.TrimSpace(req.Name), strings.ToLower(strings.TrimSpace(req.Email))
	if req.Name == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "nama wajib diisi")
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "format email tidak valid")
	}
	if len(req.Password) < 8 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "password minimal 8 karakter")
	}

	var role models.Role
	if err := h.DB.First(&role, "name = ?", enums.RoleAdmin).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "role admin belum ada")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "gagal hash password")
	}
	admin := models.User{Name: req.Name, Email: req.Email, Password: string(hash), RoleID: &role.ID, IsActive: true}
	if err := h.DB.Create(&admin).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(err.Error(), "Duplicate") {
			return fiber.NewError(fiber.StatusConflict, "email sudah terdaftar")
		}
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": fiber.Map{"email": admin.Email}})
}

// Replace — PUT /api/license (Admin): a new license (renewal, or the RS was
// renamed). The RS profile takes the new license's name.
func (h *LicenseHandler) Replace(c *fiber.Ctx) error {
	var req licenseRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	info, err := license.Parse(req.Key)
	if err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "Kode lisensi tidak valid. Periksa lagi kodenya.")
	}
	if err := saveLicense(h.DB, req.Key, info); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": licenseJSON(info)})
}
