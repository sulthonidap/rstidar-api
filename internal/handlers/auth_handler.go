package handlers

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"medsend-api/internal/auth"
	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

type AuthHandler struct {
	DB        *gorm.DB
	JWTSecret string
	JWTTTL    time.Duration
}

// rememberMeTTL is the token lifetime when the user ticks "Ingat aku".
const rememberMeTTL = 30 * 24 * time.Hour

type loginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
	Remember bool   `json:"remember"`
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if req.Email == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "email & password wajib diisi")
	}

	user, err := h.checkCredentials(req.Email, req.Password)
	if err != nil {
		return err
	}

	if !user.RoleName().CanAccessAdminPanel() {
		return fiber.NewError(fiber.StatusForbidden, "role ini tidak bisa login ke panel staff")
	}

	ttl := h.JWTTTL
	if req.Remember {
		ttl = rememberMeTTL
	}

	token, err := auth.GenerateToken(user.ID, h.JWTSecret, ttl)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "gagal membuat token")
	}

	return c.JSON(fiber.Map{
		"token": token,
		"user":  user,
	})
}

// checkCredentials — email+password cocok dan akun aktif. Dipakai login staff & kurir.
// Sebelum aplikasi diaktivasi lisensi, tidak ada yang bisa login.
func (h *AuthHandler) checkCredentials(email, password string) (*models.User, error) {
	if _, err := activeLicense(h.DB); err != nil {
		return nil, err
	}
	var user models.User
	if err := h.DB.Preload("Role").Preload("Organization").
		Where("email = ?", email).First(&user).Error; err != nil {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "email atau password salah")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "email atau password salah")
	}
	if !user.IsActive {
		return nil, fiber.NewError(fiber.StatusForbidden, "akun nonaktif")
	}
	return &user, nil
}

type courierLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// CourierLogin — POST /api/courier/login, khusus aplikasi kurir (role Kurir
// dengan data kurir aktif). Token 30 hari supaya kurir tidak login ulang tiap hari.
func (h *AuthHandler) CourierLogin(c *fiber.Ctx) error {
	var req courierLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	if req.Email == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "email & password wajib diisi")
	}

	user, err := h.checkCredentials(req.Email, req.Password)
	if err != nil {
		return err
	}
	if !user.HasRole(enums.RoleKurir) {
		return fiber.NewError(fiber.StatusForbidden, "aplikasi ini khusus kurir")
	}

	var courier models.Courier
	if err := h.DB.Preload("Organization").Where("user_id = ?", user.ID).First(&courier).Error; err != nil {
		return fiber.NewError(fiber.StatusForbidden, "akun ini belum terdaftar sebagai kurir")
	}
	if !courier.IsActive {
		return fiber.NewError(fiber.StatusForbidden, "data kurir nonaktif")
	}

	token, err := auth.GenerateToken(user.ID, h.JWTSecret, rememberMeTTL)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "gagal membuat token")
	}
	courier.User = user
	return c.JSON(fiber.Map{"token": token, "courier": courier})
}

func (h *AuthHandler) Me(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)
	return c.JSON(fiber.Map{"data": user})
}
