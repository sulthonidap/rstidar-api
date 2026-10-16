// Package realtime pushes live order events to the staff panels connected to
// GET /api/events (Server-Sent Events).
//
// The hub is in-memory, so it only reaches browsers connected to this API
// process. That's fine for a single instance; running several replicas would
// need a shared broker (e.g. Redis pub/sub) instead.
package realtime

import (
	"sync"
	"time"

	"medsend-api/internal/enums"
)

// OrderEvent is what a panel receives when an order needs someone's attention.
type OrderEvent struct {
	Type             string            `json:"type"` // "order.created" or "order.status"
	OrderID          uint              `json:"order_id"`
	Code             string            `json:"code"`
	Status           enums.OrderStatus `json:"status"`
	OrganizationID   *uint             `json:"organization_id"`
	OrganizationName string            `json:"organization_name"`
	CustomerName     string            `json:"customer_name"`
	Source           string            `json:"source"`   // "public" (form) or "cco" (staff)
	ActorID          *uint             `json:"actor_id"` // who caused it; nil for the public form
	CreatedAt        time.Time         `json:"created_at"`

	// Who gets it. Not sent to the browser.
	Roles []enums.UserRole `json:"-"`
}

type subscriber struct {
	ch     chan OrderEvent
	userID uint
	role   enums.UserRole
}

// Hub fans events out to the staff with the given roles. Whoever caused an
// event isn't pinged about it.
type Hub struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[*subscriber]struct{}{}}
}

// Subscribe registers a listener; call the returned func to unsubscribe.
func (h *Hub) Subscribe(userID uint, role enums.UserRole) (<-chan OrderEvent, func()) {
	s := &subscriber{ch: make(chan OrderEvent, 16), userID: userID, role: role}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, s)
			h.mu.Unlock()
		})
	}
}

// Publish never blocks the request that triggered it: a subscriber whose
// buffer is full (stalled connection) just misses the event.
func (h *Hub) Publish(e OrderEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs {
		if e.ActorID != nil && *e.ActorID == s.userID {
			continue
		}
		if !hasRole(e.Roles, s.role) {
			continue
		}
		select {
		case s.ch <- e:
		default:
		}
	}
}

func hasRole(roles []enums.UserRole, role enums.UserRole) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// NotifyRoles: who needs to act when an order reaches a status.
//   - new order        → CCO validates it
//   - confirmed        → farmasi prepares the medicine, Ops assigns a courier
//   - ready for pickup → CCO goes to collect it
//   - packed           → Ops makes sure a courier is assigned to take it
func NotifyRoles(status enums.OrderStatus) []enums.UserRole {
	switch status {
	case enums.OrderStatusPending:
		return []enums.UserRole{enums.RoleCco}
	case enums.OrderStatusConfirmed:
		return []enums.UserRole{enums.RoleFarmasi, enums.RoleOps}
	case enums.OrderStatusReadyForPickup:
		return []enums.UserRole{enums.RoleCco}
	case enums.OrderStatusPacked:
		return []enums.UserRole{enums.RoleOps}
	}
	return nil
}
