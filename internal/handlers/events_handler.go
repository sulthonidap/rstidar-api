package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"medsend-api/internal/enums"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
	"medsend-api/internal/privacy"
	"medsend-api/internal/realtime"
)

// heartbeatEvery keeps idle connections alive through proxies (Cloudflare drops
// connections that send nothing for ~100s).
const heartbeatEvery = 25 * time.Second

type EventsHandler struct {
	Hub *realtime.Hub
}

// Stream — GET /api/events: Server-Sent Events stream of the order events the
// logged-in CCO/Farmasi/Ops/Finance must act on (see realtime.Hub).
func (h *EventsHandler) Stream(c *fiber.Ctx) error {
	user := middleware.CurrentUser(c)
	events, unsubscribe := h.Hub.Subscribe(user.ID, user.RoleName())

	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set(fiber.HeaderConnection, "keep-alive")
	c.Set("X-Accel-Buffering", "no") // don't let a reverse proxy buffer the stream

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		defer unsubscribe()
		ticker := time.NewTicker(heartbeatEvery)
		defer ticker.Stop()

		fmt.Fprint(w, "retry: 5000\n: connected\n\n")
		if w.Flush() != nil {
			return
		}
		for {
			select {
			case e := <-events:
				data, _ := json.Marshal(e)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, data)
			case <-ticker.C:
				fmt.Fprint(w, ": ping\n\n")
			}
			// A failed flush means the browser went away.
			if w.Flush() != nil {
				return
			}
		}
	})
	return nil
}

// publishOrderEvent tells the staff who must act next (realtime.NotifyRoles)
// about an order that was just created ("order.created") or moved ("order.status").
// order.Organization must be loaded for the RS name.
// roles overrides who is told (default: realtime.NotifyRoles for the status).
func publishOrderEvent(hub *realtime.Hub, eventType string, order *models.Order, actorID *uint, roles ...enums.UserRole) {
	if len(roles) == 0 {
		roles = realtime.NotifyRoles(order.Status)
	}
	if hub == nil || len(roles) == 0 {
		return
	}
	orgName := ""
	if order.Organization != nil {
		orgName = order.Organization.Name
	}
	hub.Publish(realtime.OrderEvent{
		Type:             eventType,
		OrderID:          order.ID,
		Code:             order.Code,
		Status:           order.Status,
		OrganizationID:   order.OrganizationID,
		OrganizationName: orgName,
		CustomerName:     privacy.Name(order.CustomerName), // notifikasi panel ikut disamarkan
		Source:           string(order.Source),
		ActorID:          actorID,
		CreatedAt:        order.CreatedAt,
		Roles:            roles,
	})
}
