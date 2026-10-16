package handlers

import (
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/database"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
	"medsend-api/internal/storage"
)

// ResetHandler — factory reset for demos: empties every table (license and
// admin included) so the app is back at "Aktivasi lisensi" + "Buat Akun Admin".
// Only registered when ALLOW_FACTORY_RESET=true.
type ResetHandler struct {
	DB    *gorm.DB
	Files storage.Storage
}

// factoryResetConfirm must be sent as {"confirm": "..."} so a stray request can't wipe the data.
const factoryResetConfirm = "HAPUS SEMUA DATA"

// FactoryReset — POST /api/factory-reset (Admin), body {"confirm":"HAPUS SEMUA DATA"}.
func (h *ResetHandler) FactoryReset(c *fiber.Ctx) error {
	var body struct {
		Confirm string `json:"confirm"`
	}
	_ = c.BodyParser(&body)
	if body.Confirm != factoryResetConfirm {
		return fiber.NewError(fiber.StatusUnprocessableEntity,
			fmt.Sprintf("kirim {\"confirm\": %q} untuk menghapus semua data", factoryResetConfirm))
	}

	user := middleware.CurrentUser(c)
	log.Printf("factory reset: dimulai oleh %s (id %d)", user.Email, user.ID)

	// Collect the stored files first; their keys live in the tables we're about to empty.
	keys := h.fileKeys()

	tables, err := truncateAllTables(h.DB)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "gagal mengosongkan database: "+err.Error())
	}
	// Roles are fixed data the app needs before the first admin exists.
	if err := database.Bootstrap(h.DB); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "database kosong, tapi gagal membuat role: "+err.Error())
	}

	// Files are best-effort: a leftover photo is harmless, the data is already gone.
	failed := 0
	for _, k := range keys {
		if err := h.Files.Delete(c.UserContext(), k); err != nil {
			failed++
		}
	}
	log.Printf("factory reset: %d tabel dikosongkan, %d file dihapus, %d gagal", len(tables), len(keys)-failed, failed)

	return c.JSON(fiber.Map{
		"message":       "Semua data dihapus. Buka panel untuk aktivasi lisensi dan buat akun admin baru.",
		"tables":        tables,
		"files_deleted": len(keys) - failed,
		"files_failed":  failed,
	})
}

// fileKeys lists every uploaded file: proof-of-delivery photos/thumbnails and the QRIS image.
func (h *ResetHandler) fileKeys() []string {
	var keys []string
	var deliveries []models.OrderDelivery
	h.DB.Select("photos", "thumbs").Find(&deliveries)
	for _, d := range deliveries {
		keys = append(keys, d.Photos...)
		keys = append(keys, d.Thumbs...)
	}
	var qris models.AppSetting
	if h.DB.Where("name = ?", setQRIS).Limit(1).Find(&qris).RowsAffected > 0 && qris.Value != nil && *qris.Value != "" {
		keys = append(keys, *qris.Value)
	}
	return keys
}

// truncateAllTables empties every table except the migration bookkeeping, on
// one connection so FOREIGN_KEY_CHECKS=0 applies to all the TRUNCATEs.
func truncateAllTables(db *gorm.DB) ([]string, error) {
	var tables []string
	err := db.Connection(func(conn *gorm.DB) error {
		rows, err := conn.Raw("SHOW TABLES").Rows()
		if err != nil {
			return err
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			if name != "schema_migrations" {
				tables = append(tables, name)
			}
		}
		rows.Close()

		if err := conn.Exec("SET FOREIGN_KEY_CHECKS = 0").Error; err != nil {
			return err
		}
		defer conn.Exec("SET FOREIGN_KEY_CHECKS = 1")
		for _, t := range tables {
			if err := conn.Exec("TRUNCATE TABLE `" + t + "`").Error; err != nil {
				return fmt.Errorf("%s: %w", t, err)
			}
		}
		return nil
	})
	return tables, err
}
