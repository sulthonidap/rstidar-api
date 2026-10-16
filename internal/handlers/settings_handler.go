package handlers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"medsend-api/internal/models"
	"medsend-api/internal/storage"
)

// SettingsHandler — Admin settings; for now where non-cash payments go.
type SettingsHandler struct {
	DB    *gorm.DB
	Files storage.Storage
}

const (
	setBankName      = "payment.bank_name"
	setAccountNo     = "payment.account_no"
	setAccountName   = "payment.account_name"
	setInstructions  = "payment.instructions"
	setQRIS          = "payment.qris" // storage key of the QRIS image
	maxQRISBytes     = 3 << 20
	maxSettingLength = 500
)

func (h *SettingsHandler) get(names ...string) map[string]string {
	var rows []models.AppSetting
	h.DB.Where("name IN ?", names).Find(&rows)
	out := map[string]string{}
	for _, r := range rows {
		if r.Value != nil {
			out[r.Name] = *r.Value
		}
	}
	return out
}

func (h *SettingsHandler) set(tx *gorm.DB, name, value string) error {
	v := strings.TrimSpace(value)
	row := models.AppSetting{Name: name, Value: &v}
	if v == "" {
		row.Value = nil
	}
	return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error
}

// paymentInfo: what a non-cash payer needs (the QRIS as a loadable URL).
func (h *SettingsHandler) paymentInfo() fiber.Map {
	s := h.get(setBankName, setAccountNo, setAccountName, setInstructions, setQRIS)
	var qris *string
	if key := s[setQRIS]; key != "" {
		u := h.Files.URL(key)
		qris = &u
	}
	return fiber.Map{
		"bank_name":    s[setBankName],
		"account_no":   s[setAccountNo],
		"account_name": s[setAccountName],
		"instructions": s[setInstructions],
		"qris_url":     qris,
	}
}

// PublicPayment — GET /api/public/payment-info (shown with a non-cash order).
func (h *SettingsHandler) PublicPayment(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"data": h.paymentInfo()})
}

// Payment — GET /api/settings/payment (Admin).
func (h *SettingsHandler) Payment(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"data": h.paymentInfo()})
}

type paymentSettingsRequest struct {
	BankName     string `json:"bank_name"`
	AccountNo    string `json:"account_no"`
	AccountName  string `json:"account_name"`
	Instructions string `json:"instructions"`
}

// UpdatePayment — PUT /api/settings/payment (Admin): the bank account.
func (h *SettingsHandler) UpdatePayment(c *fiber.Ctx) error {
	var req paymentSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	for _, v := range []string{req.BankName, req.AccountNo, req.AccountName, req.Instructions} {
		if len(v) > maxSettingLength {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "isian terlalu panjang")
		}
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		for name, v := range map[string]string{
			setBankName: req.BankName, setAccountNo: req.AccountNo,
			setAccountName: req.AccountName, setInstructions: req.Instructions,
		} {
			if err := h.set(tx, name, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": h.paymentInfo()})
}

// UploadQRIS — POST /api/settings/payment/qris (Admin, multipart "qris").
// Stored as uploaded (no re-compression, so the code stays scannable).
func (h *SettingsHandler) UploadQRIS(c *fiber.Ctx) error {
	fh, err := c.FormFile("qris")
	if err != nil {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "pilih gambar QRIS")
	}
	if fh.Size > maxQRISBytes {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "gambar QRIS maksimal 3 MB")
	}
	f, err := fh.Open()
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "gambar tidak bisa dibaca")
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "gambar tidak bisa dibaca")
	}
	contentType := http.DetectContentType(data)
	ext := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp"}[contentType]
	if ext == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "QRIS harus PNG/JPG/WEBP")
	}

	key := fmt.Sprintf("settings/qris-%d-%s%s", time.Now().Unix(), randomHex(6), ext)
	if err := h.Files.Save(c.UserContext(), key, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "gagal menyimpan gambar")
	}
	old := h.get(setQRIS)[setQRIS]
	if err := h.set(h.DB, setQRIS, key); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	if old != "" {
		_ = h.Files.Delete(c.UserContext(), old)
	}
	return c.JSON(fiber.Map{"data": h.paymentInfo()})
}

// DeleteQRIS — DELETE /api/settings/payment/qris (Admin).
func (h *SettingsHandler) DeleteQRIS(c *fiber.Ctx) error {
	if old := h.get(setQRIS)[setQRIS]; old != "" {
		_ = h.Files.Delete(c.UserContext(), old)
	}
	if err := h.set(h.DB, setQRIS, ""); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": h.paymentInfo()})
}

// SetupChecklist — GET /api/settings/setup-checklist (Admin): what a fresh
// install still lacks (services, an active account per staff role, non-cash
// payment), so the panel can show the first-run checklist. The panel decides
// which items are required.
func (h *SettingsHandler) SetupChecklist(c *fiber.Ctx) error {
	var services int64
	if err := h.DB.Model(&models.Service{}).Count(&services).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}

	var rows []struct {
		Name  string
		Count int64
	}
	if err := h.DB.Table("users").
		Select("roles.name AS name, COUNT(*) AS count").
		Joins("JOIN roles ON roles.id = users.role_id").
		Where("users.is_active = ?", true).
		Group("roles.name").
		Scan(&rows).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	users := map[string]int64{}
	for _, r := range rows {
		users[r.Name] = r.Count
	}

	s := h.get(setBankName, setAccountNo, setQRIS)
	payment := (s[setBankName] != "" && s[setAccountNo] != "") || s[setQRIS] != ""

	return c.JSON(fiber.Map{"data": fiber.Map{
		"services":           services,
		"users":              users,
		"payment_configured": payment,
	}})
}
