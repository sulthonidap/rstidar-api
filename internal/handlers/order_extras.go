package handlers

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
)

const maxScheduleDays = 60

// orderExtras: the delivery schedule and the patient (name + medical record
// number, required on every channel: kiosk, public form, staff create/edit).
// The public form always sends as-soon-as-possible.
type orderExtras struct {
	ScheduledDate   string  `json:"scheduled_date"` // "YYYY-MM-DD"; empty = secepatnya
	ScheduledSlot   string  `json:"scheduled_slot"` // pagi / siang / sore
	PatientName     *string `json:"patient_name"`
	MedicalRecordNo *string `json:"medical_record_no"`
	// Destination map point (optional); both null = unknown.
	DropoffLat *float64 `json:"dropoff_lat"`
	DropoffLng *float64 `json:"dropoff_lng"`
}

func trimOptional(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

// schedule validates the schedule: a date from today up to 60 days ahead with a
// slot, or nothing. current is the order's saved date when editing: keeping it
// is fine even once it has passed.
func (x *orderExtras) schedule(current *models.Date) (*models.Date, *enums.ScheduleSlot, error) {
	if strings.TrimSpace(x.ScheduledDate) == "" {
		return nil, nil, nil
	}
	date, err := models.ParseDate(x.ScheduledDate)
	if err != nil {
		return nil, nil, fiber.NewError(fiber.StatusUnprocessableEntity, "tanggal kirim harus YYYY-MM-DD")
	}
	y, m, d := time.Now().Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	kept := current != nil && current.String() == date.String()
	if date.Before(today) && !kept {
		return nil, nil, fiber.NewError(fiber.StatusUnprocessableEntity, "tanggal kirim tidak boleh lewat dari hari ini")
	}
	if date.After(today.AddDate(0, 0, maxScheduleDays)) && !kept {
		return nil, nil, fiber.NewError(fiber.StatusUnprocessableEntity, "tanggal kirim maksimal 60 hari ke depan")
	}
	slot := enums.ScheduleSlot(x.ScheduledSlot)
	if !slot.Valid() {
		return nil, nil, fiber.NewError(fiber.StatusUnprocessableEntity, "pilih slot waktu kirim (pagi/siang/sore)")
	}
	return &date, &slot, nil
}

// patient validates the patient data (trimmed): the RS needs both the name and
// the medical record number to find the patient's prescription.
func (x *orderExtras) patient() (*string, *string, error) {
	name, rm := trimOptional(x.PatientName), trimOptional(x.MedicalRecordNo)
	if rm == nil {
		return nil, nil, fiber.NewError(fiber.StatusUnprocessableEntity, "No. Rekam Medis pasien wajib diisi")
	}
	if name == nil {
		return nil, nil, fiber.NewError(fiber.StatusUnprocessableEntity, "nama pasien wajib diisi")
	}
	if len(*name) > 255 || len(*rm) > 50 {
		return nil, nil, fiber.NewError(fiber.StatusUnprocessableEntity, "data pasien terlalu panjang")
	}
	return name, rm, nil
}

// dropoffPoint validates the optional destination point.
func (x *orderExtras) dropoffPoint() (*float64, *float64, error) {
	if !validPoint(x.DropoffLat, x.DropoffLng) {
		return nil, nil, fiber.NewError(fiber.StatusUnprocessableEntity, "titik tujuan harus di wilayah Indonesia")
	}
	return x.DropoffLat, x.DropoffLng, nil
}

func samePoint(aLat, aLng, bLat, bLng *float64) bool {
	if aLat == nil || aLng == nil || bLat == nil || bLng == nil {
		return (aLat == nil || aLng == nil) && (bLat == nil || bLng == nil)
	}
	const eps = 1e-7
	return abs(*aLat-*bLat) < eps && abs(*aLng-*bLng) < eps
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func sameDate(a, b *models.Date) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.String() == b.String()
}

func sameSlot(a, b *enums.ScheduleSlot) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
