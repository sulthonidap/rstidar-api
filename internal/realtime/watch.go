package realtime

import "gorm.io/gorm"

// watchedTables: writes here change what the order screens show (list, status
// counts, detail, transactions).
var watchedTables = map[string]bool{
	"orders":           true,
	"transactions":     true,
	"order_deliveries": true,
}

// WatchOrders calls h.Changed after every successful write to the order
// tables, wherever it comes from (panel, public form, courier app), so the
// panels don't depend on each handler remembering to publish.
func (h *Hub) WatchOrders(db *gorm.DB) error {
	notify := func(tx *gorm.DB) {
		if tx.Error == nil && tx.Statement.RowsAffected > 0 && watchedTables[tx.Statement.Table] {
			h.Changed()
		}
	}
	cb := db.Callback()
	if err := cb.Create().After("gorm:create").Register("realtime:orders_changed", notify); err != nil {
		return err
	}
	if err := cb.Update().After("gorm:update").Register("realtime:orders_changed", notify); err != nil {
		return err
	}
	return cb.Delete().After("gorm:delete").Register("realtime:orders_changed", notify)
}
