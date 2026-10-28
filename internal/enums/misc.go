package enums

// OrderSource: lewat form publik, diinput CCO/Admin, atau diinput Farmasi RS.
type OrderSource string

const (
	OrderSourcePublic  OrderSource = "public"
	OrderSourceCco     OrderSource = "cco"
	OrderSourceFarmasi OrderSource = "farmasi"
)

func (s OrderSource) Valid() bool {
	return s == OrderSourcePublic || s == OrderSourceCco || s == OrderSourceFarmasi
}

// PaymentStatus untuk tabel transactions.
type PaymentStatus string

const (
	PaymentStatusUnpaid   PaymentStatus = "unpaid"
	PaymentStatusPaid     PaymentStatus = "paid"
	PaymentStatusRefunded PaymentStatus = "refunded"
)

func (s PaymentStatus) Valid() bool {
	switch s {
	case PaymentStatusUnpaid, PaymentStatusPaid, PaymentStatusRefunded:
		return true
	}
	return false
}

// PaymentMethod chosen when ordering.
type PaymentMethod string

const (
	PaymentCash    PaymentMethod = "cash"    // tunai di kasir RS sebelum obat diambil
	PaymentNonCash PaymentMethod = "noncash" // transfer / QRIS + bukti, diverifikasi Finance
)

func (m PaymentMethod) Valid() bool { return m == PaymentCash || m == PaymentNonCash }

// PatientCategory: kategori pasien untuk program ongkir gratis.
type PatientCategory string

const (
	PatientUmum        PatientCategory = "umum"
	PatientLansia      PatientCategory = "lansia"
	PatientDisabilitas PatientCategory = "disabilitas"
)

func (p PatientCategory) Valid() bool {
	return p == PatientUmum || p == PatientLansia || p == PatientDisabilitas
}

// Label: "Lansia", "Disabilitas", "Umum".
func (p PatientCategory) Label() string {
	switch p {
	case PatientLansia:
		return "Lansia"
	case PatientDisabilitas:
		return "Disabilitas"
	}
	return "Umum"
}

// UserRole — nilainya sama persis dengan kolom roles.name di database.
type UserRole string

const (
	RoleAdmin   UserRole = "admin"
	RoleOps     UserRole = "ops"
	RoleCco     UserRole = "cco"
	RoleKurir   UserRole = "kurir"
	RoleFinance UserRole = "finance"
	RoleFarmasi UserRole = "farmasi" // staf farmasi RS: siapkan obat, tandai "siap diambil"
)

// CanAccessAdminPanel: role kurir tidak boleh login ke sisi admin/staff.
func (r UserRole) CanAccessAdminPanel() bool {
	return r != RoleKurir
}

// ScheduleSlot: delivery time window when an order is scheduled for a date.
type ScheduleSlot string

const (
	SlotPagi  ScheduleSlot = "pagi"  // 08–12
	SlotSiang ScheduleSlot = "siang" // 12–15
	SlotSore  ScheduleSlot = "sore"  // 15–18
)

func (s ScheduleSlot) Valid() bool {
	return s == SlotPagi || s == SlotSiang || s == SlotSore
}
