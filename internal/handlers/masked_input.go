package handlers

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"medsend-api/internal/privacy"
)

// maskedField: satu field form yang di panel tampil tersamar (lihat
// middleware.MaskPatientData).
type maskedField struct {
	in    *string // nilai dari form (nil = tidak dikirim)
	orig  string  // nilai asli di database
	mask  func(string) string
	label string
}

// restoreMasked: form panel menampilkan data pasien yang tersamar. Kalau field
// dikirim balik tanpa diubah, nilai aslinya yang dipakai — data asli tidak
// tertimpa "B*** S******". Field yang diedit setengah (masih ada '*') ditolak.
func restoreMasked(fields []maskedField) error {
	for _, f := range fields {
		if f.in == nil {
			continue
		}
		v, ok := privacy.Keep(strings.TrimSpace(*f.in), f.orig, f.mask)
		if !ok {
			return fiber.NewError(fiber.StatusUnprocessableEntity,
				f.label+" masih tersamar (*). Hapus isinya lalu ketik ulang lengkap.")
		}
		*f.in = v
	}
	return nil
}
