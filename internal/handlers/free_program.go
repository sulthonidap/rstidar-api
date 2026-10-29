package handlers

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
)

// Program ongkir gratis: pasien lansia/disabilitas yang domisilinya (mis. Kota
// Magelang) sudah dicek petugas dari KTP tidak membayar ongkir. Petugaslah yang
// memverifikasi. Admin menyalakannya terpisah untuk form petugas (enabled) dan
// form pasien & kiosk (public), dan menulis syarat domisilinya di Pengaturan.
// Dari form pasien, kategori cuma pengajuan: gratis setelah CCO mengecek KTP-nya
// lalu menyetujuinya (FreeClaim).
const (
	setFreeEnabled      = "free_program.enabled"
	setFreePublic       = "free_program.public"
	setFreeDomicile     = "free_program.domicile"
	defaultFreeDomicile = "Kota Magelang"
	// Catatan transaksi yang dilunasi otomatis karena gratis; dikenali lagi saat
	// order batal gratis, supaya transaksinya kembali "belum bayar".
	freePaymentNote = "Gratis — program ongkir gratis"
)

type freeProgram struct {
	Enabled  bool   `json:"enabled"`  // di form order petugas
	Public   bool   `json:"public"`   // di form pasien & kiosk (pengajuan)
	Domicile string `json:"domicile"` // syarat domisili, mis. "Kota Magelang"
}

// active: the program runs on either form, so CCO can verify a claim.
func (p freeProgram) active() bool { return p.Enabled || p.Public }

func loadFreeProgram(db *gorm.DB) freeProgram {
	var rows []models.AppSetting
	db.Where("name IN ?", []string{setFreeEnabled, setFreePublic, setFreeDomicile}).Find(&rows)
	p := freeProgram{Domicile: defaultFreeDomicile}
	for _, r := range rows {
		if r.Value == nil {
			continue
		}
		switch r.Name {
		case setFreeEnabled:
			p.Enabled = *r.Value == "1"
		case setFreePublic:
			p.Public = *r.Value == "1"
		case setFreeDomicile:
			if v := strings.TrimSpace(*r.Value); v != "" {
				p.Domicile = v
			}
		}
	}
	return p
}

// freeInput: isian program gratis di form order petugas (buat & edit).
// nil = tidak dikirim, nilai order tetap.
type freeInput struct {
	PatientCategory *string `json:"patient_category"`
	DomicileChecked *bool   `json:"domicile_checked"`
}

// apply menyalin kategori pasien & centang domisili dari form ke order.
func (in freeInput) apply(o *models.Order) error {
	if in.PatientCategory != nil {
		cat := enums.PatientCategory(strings.TrimSpace(*in.PatientCategory))
		if cat == "" {
			cat = enums.PatientUmum
		}
		if !cat.Valid() {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "kategori pasien tidak valid")
		}
		o.PatientCategory = cat
	}
	if in.DomicileChecked != nil {
		o.DomicileChecked = *in.DomicileChecked
	}
	if o.PatientCategory == "" || o.PatientCategory == enums.PatientUmum {
		o.PatientCategory, o.DomicileChecked = enums.PatientUmum, false
	}
	return nil
}

// eligible: program aktif, pasien lansia/disabilitas, dan domisilinya sudah dicek.
func (p freeProgram) eligible(o *models.Order) bool {
	return p.active() && o.PatientCategory != enums.PatientUmum && o.DomicileChecked
}

// publicFreeInput: kategori yang dipilih pasien sendiri di form pasien/kiosk —
// pengajuan, belum dicek petugas, jadi belum gratis.
type publicFreeInput struct {
	PatientCategory *string `json:"patient_category"`
}

func (in publicFreeInput) apply(o *models.Order) error {
	if err := (freeInput{PatientCategory: in.PatientCategory}).apply(o); err != nil {
		return err
	}
	o.DomicileChecked = false
	return nil
}

// freeClaimNote untuk riwayat order publik: "Pasien mengajukan ongkir gratis (Lansia), perlu dicek petugas".
func freeClaimNote(o *models.Order) string {
	return "Pasien mengajukan ongkir gratis (" + o.PatientCategory.Label() + "), perlu dicek petugas"
}

// applyFree dipanggil setelah applyPrice (o.Price = tarif): order gratis
// harganya 0 dan tarifnya disimpan di NormalPrice untuk laporan subsidi.
// Mencatat siapa yang memverifikasi saat order baru menjadi gratis.
func applyFree(p freeProgram, o *models.Order, userID uint) {
	wasFree := o.IsFree
	o.IsFree = p.eligible(o)
	if !o.IsFree {
		o.NormalPrice, o.FreeVerifiedBy, o.FreeVerifiedAt = nil, nil, nil
		return
	}
	normal := o.Price
	o.NormalPrice, o.Price = &normal, 0
	if !wasFree {
		now := time.Now()
		o.FreeVerifiedBy, o.FreeVerifiedAt = &userID, &now
	}
}

// freeNote untuk riwayat status: "Ongkir gratis: Lansia, domisili Kota Magelang (dicek petugas)".
func freeNote(p freeProgram, o *models.Order) string {
	return "Ongkir gratis: " + o.PatientCategory.Label() + ", domisili " + p.Domicile + " (dicek petugas)"
}

// syncFreePayment: order gratis tidak perlu dibayar — transaksinya yang belum
// lunas dilunasi otomatis Rp0. Order yang batal gratis kembali "belum bayar"
// dengan harga barunya (hanya kalau lunasnya memang karena gratis).
func syncFreePayment(db *gorm.DB, o *models.Order, userID uint) error {
	var txn models.Transaction
	if db.Where("order_id = ?", o.ID).First(&txn).Error != nil {
		return nil
	}
	switch {
	case o.IsFree && txn.PaymentStatus == enums.PaymentStatusUnpaid:
		now := time.Now()
		return db.Model(&txn).Updates(map[string]any{
			"amount": 0, "payment_status": enums.PaymentStatusPaid, "paid_at": now,
			"recorded_by": userID, "note": freePaymentNote,
		}).Error
	case !o.IsFree && paidBecauseFree(txn):
		return db.Model(&txn).Updates(map[string]any{
			"amount": o.Price, "payment_status": enums.PaymentStatusUnpaid, "paid_at": nil,
			"recorded_by": nil, "note": nil,
		}).Error
	}
	return nil
}

func paidBecauseFree(t models.Transaction) bool {
	return t.PaymentStatus == enums.PaymentStatusPaid && t.Note != nil && *t.Note == freePaymentNote
}

// paidByPatient: pasien benar-benar sudah membayar (bukan lunas otomatis karena gratis).
func paidByPatient(db *gorm.DB, orderID uint) bool {
	var txn models.Transaction
	if db.Where("order_id = ?", orderID).First(&txn).Error != nil {
		return false
	}
	return txn.PaymentStatus == enums.PaymentStatusPaid && !paidBecauseFree(txn)
}

// FreeProgram — GET /api/free-program (semua staff): untuk form order.
func (h *SettingsHandler) FreeProgram(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"data": loadFreeProgram(h.DB)})
}

// UpdateFreeProgram — PUT /api/settings/free-program (Admin).
func (h *SettingsHandler) UpdateFreeProgram(c *fiber.Ctx) error {
	var req freeProgram
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	req.Domicile = strings.TrimSpace(req.Domicile)
	if req.Domicile == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "syarat domisili wajib diisi, mis. Kota Magelang")
	}
	if len(req.Domicile) > 100 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "syarat domisili terlalu panjang")
	}
	flag := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := h.set(tx, setFreeEnabled, flag(req.Enabled)); err != nil {
			return err
		}
		if err := h.set(tx, setFreePublic, flag(req.Public)); err != nil {
			return err
		}
		return h.set(tx, setFreeDomicile, req.Domicile)
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": loadFreeProgram(h.DB)})
}

// PublicFreeProgram — GET /api/public/free-program: whether the public form &
// kiosk ask for the patient category, and the domicile requirement to show.
func (h *SettingsHandler) PublicFreeProgram(c *fiber.Ctx) error {
	p := loadFreeProgram(h.DB)
	return c.JSON(fiber.Map{"data": fiber.Map{"enabled": p.Public, "domicile": p.Domicile}})
}

// isFreeClaim: the patient asked for free delivery on the public form/kiosk and
// nobody has decided yet.
func isFreeClaim(o *models.Order) bool {
	return o.PatientCategory != "" && o.PatientCategory != enums.PatientUmum && !o.DomicileChecked && !o.IsFree
}

type freeClaimRequest struct {
	Action string  `json:"action"` // approve | reject
	Note   *string `json:"note"`
}

// FreeClaim — POST /api/orders/:id/free-claim {action, note} (CCO/Admin): the
// CCO checks the patient's KTP and decides the claim — approve makes the order
// free (paid Rp0), reject turns it into a regular (umum) order. Written to the
// status history.
func (h *OrderHandler) FreeClaim(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)
	var order models.Order
	if err := h.DB.First(&order, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "order tidak ditemukan")
	}
	if !isFreeClaim(&order) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "order ini tidak sedang mengajukan ongkir gratis")
	}
	if order.Status.IsFinal() {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "order sudah selesai/batal")
	}
	var req freeClaimRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}
	note := ""
	if req.Note != nil {
		note = strings.TrimSpace(*req.Note)
	}
	if len([]rune(note)) > 150 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "catatan maksimal 150 karakter")
	}

	program := loadFreeProgram(h.DB)
	var history string
	switch req.Action {
	case "approve":
		if !program.active() {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "program ongkir gratis sedang dimatikan Admin")
		}
		if paidByPatient(h.DB, order.ID) {
			return fiber.NewError(fiber.StatusUnprocessableEntity,
				"pasien sudah membayar "+rupiah(order.Price)+": refund dulu kalau ongkirnya memang digratiskan")
		}
		order.DomicileChecked = true
		applyFree(program, &order, user.ID)
		history = freeNote(program, &order)
	case "reject":
		history = "Pengajuan ongkir gratis (" + order.PatientCategory.Label() + ") ditolak"
		order.PatientCategory, order.DomicileChecked = enums.PatientUmum, false
	default:
		return fiber.NewError(fiber.StatusUnprocessableEntity, "aksi tidak dikenal")
	}
	if note != "" {
		history += ". Catatan: " + note
	}

	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if err := syncFreePayment(tx, &order, user.ID); err != nil {
			return err
		}
		// Same from/to status marks an edit, not a step.
		return tx.Create(&models.OrderStatusHistory{
			OrderID: order.ID, FromStatus: &order.Status, ToStatus: order.Status,
			ChangedBy: &user.ID, Note: &history, ChangedAt: time.Now(),
		}).Error
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	orderPreload(h.DB).First(&order, order.ID)
	return c.JSON(fiber.Map{"data": order})
}
