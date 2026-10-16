package enums

// OrderStatus mengikuti alur:
//
//	Pending ─CCO─► Confirmed ─Farmasi─► ReadyForPickup ─CCO (ambil + kemas)─► Packed
//	  ─Kurir (scan label)─► OnDelivery ─Kurir─► Done
//
// Cancelled bisa dari status manapun yang belum final. Penugasan kurir oleh Ops
// bukan status: courier_id diisi kapan saja antara Confirmed dan Packed.
type OrderStatus string

const (
	OrderStatusPending        OrderStatus = "pending"
	OrderStatusConfirmed      OrderStatus = "confirmed"
	OrderStatusReadyForPickup OrderStatus = "ready_for_pickup" // farmasi: obat siap diambil
	OrderStatusPacked         OrderStatus = "packed"           // CCO sudah ambil dari RS + packing, siap kirim
	OrderStatusOnDelivery     OrderStatus = "on_delivery"      // kurir sudah terima paket
	OrderStatusDone           OrderStatus = "done"
	OrderStatusCancelled      OrderStatus = "cancelled"
)

var orderStatusTransitions = map[OrderStatus][]OrderStatus{
	OrderStatusPending:        {OrderStatusConfirmed, OrderStatusCancelled},
	OrderStatusConfirmed:      {OrderStatusReadyForPickup, OrderStatusCancelled},
	OrderStatusReadyForPickup: {OrderStatusPacked, OrderStatusCancelled},
	OrderStatusPacked:         {OrderStatusOnDelivery, OrderStatusCancelled},
	OrderStatusOnDelivery:     {OrderStatusDone, OrderStatusCancelled},
	OrderStatusDone:           {},
	OrderStatusCancelled:      {},
}

var orderStatusLabels = map[OrderStatus]string{
	OrderStatusPending:        "Menunggu konfirmasi",
	OrderStatusConfirmed:      "Dikonfirmasi",
	OrderStatusReadyForPickup: "Siap diambil",
	OrderStatusPacked:         "Siap kirim",
	OrderStatusOnDelivery:     "Dalam pengiriman",
	OrderStatusDone:           "Selesai",
	OrderStatusCancelled:      "Dibatalkan",

	// Status lama yang masih bisa muncul di riwayat order.
	"collected": "Diambil dari RS",
	"assigned":  "Kurir ditugaskan",
}

// staffMoves: which staff role may move an order INTO a status from the web
// panel. Admin may do all of these (for emergencies). The courier steps
// (OnDelivery, Done) are only done by the assigned courier from the app.
var staffMoves = map[OrderStatus][]UserRole{
	OrderStatusConfirmed:      {RoleCco},
	OrderStatusReadyForPickup: {RoleFarmasi},
	OrderStatusPacked:         {RoleCco},
	OrderStatusCancelled:      {RoleCco},
}

// AssignableStatuses: Ops can (re)assign the courier from confirmation until
// the courier has taken the package.
var AssignableStatuses = []OrderStatus{
	OrderStatusConfirmed, OrderStatusReadyForPickup, OrderStatusPacked,
}

// ActiveCourierStatuses: orders a courier still has to work on (they may be
// assigned early, so this includes the steps before they get the package).
var ActiveCourierStatuses = append(append([]OrderStatus{}, AssignableStatuses...), OrderStatusOnDelivery)

func (s OrderStatus) Label() string {
	if l, ok := orderStatusLabels[s]; ok {
		return l
	}
	return string(s)
}

func (s OrderStatus) Valid() bool {
	_, ok := orderStatusTransitions[s]
	return ok
}

// AllowedTransitions balikin status yang boleh dituju dari status sekarang.
func (s OrderStatus) AllowedTransitions() []OrderStatus {
	return orderStatusTransitions[s]
}

func (s OrderStatus) CanTransitionTo(target OrderStatus) bool {
	for _, t := range s.AllowedTransitions() {
		if t == target {
			return true
		}
	}
	return false
}

// StaffCanMove: may this staff role move an order from s to target on the
// web panel? Only the admin may cancel once the courier has the package.
func (s OrderStatus) StaffCanMove(role UserRole, target OrderStatus) bool {
	if role == RoleAdmin {
		_, staffStep := staffMoves[target]
		return staffStep
	}
	if target == OrderStatusCancelled && s == OrderStatusOnDelivery {
		return false
	}
	for _, r := range staffMoves[target] {
		if r == role {
			return true
		}
	}
	return false
}

func (s OrderStatus) IsAssignable() bool {
	for _, a := range AssignableStatuses {
		if a == s {
			return true
		}
	}
	return false
}

func (s OrderStatus) IsFinal() bool {
	return len(s.AllowedTransitions()) == 0
}

// TimestampColumn balikin nama kolom timestamp di orders yang diisi
// saat order masuk status ini. String kosong kalau tidak ada.
func (s OrderStatus) TimestampColumn() string {
	switch s {
	case OrderStatusConfirmed:
		return "confirmed_at"
	case OrderStatusReadyForPickup:
		return "ready_at"
	case OrderStatusPacked:
		return "packed_at"
	case OrderStatusOnDelivery:
		return "picked_up_at"
	case OrderStatusDone:
		return "delivered_at"
	default:
		return ""
	}
}
