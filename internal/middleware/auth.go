package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/auth"
	"medsend-api/internal/enums"
	"medsend-api/internal/models"
)

const UserContextKey = "user"

// RequireAuth parse Bearer token, load User (+Role +Organization), dan
// simpan ke c.Locals(UserContextKey). Analog middleware auth:sanctum.
func RequireAuth(db *gorm.DB, secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
		}

		tokenStr := strings.TrimPrefix(header, "Bearer ")
		claims, err := auth.ParseToken(tokenStr, secret)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "token tidak valid")
		}

		var user models.User
		if err := db.Preload("Role").Preload("Organization").First(&user, claims.UserID).Error; err != nil {
			return fiber.NewError(fiber.StatusUnauthorized, "user tidak ditemukan")
		}

		if !user.IsActive {
			return fiber.NewError(fiber.StatusForbidden, "akun nonaktif")
		}

		c.Locals(UserContextKey, &user)
		return c.Next()
	}
}

// RequireRole menolak request kalau user tidak punya salah satu role ini.
// Harus dipasang setelah RequireAuth.
func RequireRole(roles ...enums.UserRole) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals(UserContextKey).(*models.User)
		if !ok || user == nil {
			return fiber.NewError(fiber.StatusUnauthorized, "unauthenticated")
		}
		if !user.HasAnyRole(roles...) {
			return fiber.NewError(fiber.StatusForbidden, "tidak punya akses")
		}
		return c.Next()
	}
}

const CourierContextKey = "courier"

// RequireCourier — untuk endpoint aplikasi kurir: user login harus punya data
// kurir yang aktif; kurirnya disimpan ke context. Pasang setelah RequireAuth.
func RequireCourier(db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user := CurrentUser(c)
		if user == nil || !user.HasRole(enums.RoleKurir) {
			return fiber.NewError(fiber.StatusForbidden, "khusus kurir")
		}
		var courier models.Courier
		if err := db.Preload("Organization").Where("user_id = ?", user.ID).First(&courier).Error; err != nil {
			return fiber.NewError(fiber.StatusForbidden, "akun ini belum terdaftar sebagai kurir")
		}
		if !courier.IsActive {
			return fiber.NewError(fiber.StatusForbidden, "data kurir nonaktif")
		}
		c.Locals(CourierContextKey, &courier)
		return c.Next()
	}
}

// CurrentCourier ambil kurir yang login (hanya di route yang pakai RequireCourier).
func CurrentCourier(c *fiber.Ctx) *models.Courier {
	courier, _ := c.Locals(CourierContextKey).(*models.Courier)
	return courier
}

// CurrentUser ambil user yang login dari context request.
func CurrentUser(c *fiber.Ctx) *models.User {
	user, _ := c.Locals(UserContextKey).(*models.User)
	return user
}
