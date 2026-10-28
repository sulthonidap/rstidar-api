package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"medsend-api/internal/privacy"
)

// MaskPatientData menyamarkan data pasien di setiap respons JSON panel, untuk
// semua role (termasuk Admin). Dikecualikan:
//   - /api/courier/...  aplikasi kurir: data lengkap untuk order miliknya
//   - /api/orders/labels  label/resi: dicetak lengkap untuk ditempel di paket
//
// Ekspor Excel dan notifikasi realtime disamarkan di handler-nya masing-masing.
// Pasang setelah RequireAuth.
func MaskPatientData() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := c.Next(); err != nil {
			return err
		}
		path := c.Path()
		if strings.HasPrefix(path, "/api/courier/") || path == "/api/orders/labels" {
			return nil
		}
		if !strings.HasPrefix(string(c.Response().Header.ContentType()), fiber.MIMEApplicationJSON) {
			return nil
		}
		body := c.Response().Body()
		if len(body) == 0 {
			return nil
		}
		masked, err := privacy.MaskJSON(body)
		if err != nil {
			// Jangan pernah kirim data mentah kalau masking gagal.
			return fiber.NewError(fiber.StatusInternalServerError, "gagal menyiapkan data")
		}
		c.Response().SetBodyRaw(masked)
		return nil
	}
}
