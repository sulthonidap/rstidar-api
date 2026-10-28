package routes

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/handlers"
	"medsend-api/internal/middleware"
	"medsend-api/internal/realtime"
	"medsend-api/internal/routing"
	"medsend-api/internal/storage"
)

func Register(app *fiber.App, db *gorm.DB, store storage.Storage, router routing.Router, jwtSecret string, jwtTTL time.Duration, allowFactoryReset bool) {
	authH := &handlers.AuthHandler{DB: db, JWTSecret: jwtSecret, JWTTTL: jwtTTL}
	// One hub per process: order handlers publish, /api/events streams to panels.
	hub := realtime.NewHub()
	orderH := handlers.NewOrderHandler(db, hub, store, router)
	eventsH := &handlers.EventsHandler{Hub: hub}
	orgH := &handlers.OrganizationHandler{DB: db}
	svcH := &handlers.ServiceHandler{DB: db}
	custH := &handlers.CustomerHandler{DB: db}
	reportH := &handlers.ReportHandler{DB: db}
	courierH := &handlers.CourierHandler{DB: db}
	userH := &handlers.UserHandler{DB: db}
	txnH := &handlers.TransactionHandler{DB: db, Files: store}
	settingsH := &handlers.SettingsHandler{DB: db, Files: store}
	pubH := &handlers.PublicHandler{DB: db, Hub: hub, Store: store, Router: router}
	roleH := &handlers.RoleHandler{DB: db}
	licenseH := &handlers.LicenseHandler{DB: db}
	courierAppH := handlers.NewCourierAppHandler(db, store)
	routesH := &handlers.CourierRoutesHandler{DB: db, Router: router}

	api := app.Group("/api")

	// --- Publik (tanpa login) ---
	pub := api.Group("/public")
	// Pertama kali: aktivasi lisensi lalu buat akun admin (sebelum ada yang bisa login).
	pub.Get("/setup", licenseH.Status)
	pub.Post("/activate", licenseH.Activate)
	pub.Post("/setup-admin", licenseH.SetupAdmin)
	pub.Get("/services", pubH.Services)
	pub.Get("/organization", pubH.Organization) // profil RS untuk header form order
	pub.Post("/orders", pubH.StoreOrder)
	pub.Get("/orders/:code", pubH.Track)
	pub.Get("/payment-info", settingsH.PublicPayment)                                       // rekening & QRIS untuk non-tunai
	pub.Post("/quote", pubH.Quote)                                                          // estimasi harga + jarak jalan untuk form order
	pub.Get("/maps-link", pubH.MapsLink)                                                    // koordinat dari link Google Maps (termasuk link pendek)
	pub.Post("/returning-patient", handlers.ReturningPatientLimit(), pubH.ReturningPatient) // isi otomatis "pasien lama" (HP + No. RM)

	api.Post("/auth/login", authH.Login)
	api.Post("/courier/login", authH.CourierLogin) // aplikasi kurir

	// --- Butuh login ---
	// Data pasien disamarkan di semua respons panel (kecuali aplikasi kurir & label).
	auth := api.Group("", middleware.RequireAuth(db, jwtSecret), middleware.MaskPatientData())
	auth.Get("/auth/me", authH.Me)
	// Live "order baru" notifications (Server-Sent Events) — only the CCO of the
	// ordering RS validates new orders, so only CCOs subscribe.
	auth.Get("/events", middleware.RequireRole(enums.RoleCco, enums.RoleFarmasi, enums.RoleOps, enums.RoleFinance), eventsH.Stream)

	// --- Aplikasi kurir: cuma order yang di-assign ke kurir yang login ---
	// RequireCourier is attached per route, not on the group: Fiber group
	// middleware matches by path *prefix*, so a "/courier" group middleware would
	// also catch the admin "/couriers" routes and reject them with 403.
	courierApp := auth.Group("/courier")
	onlyCourier := middleware.RequireCourier(db)
	courierApp.Get("/me", onlyCourier, courierAppH.Me)
	courierApp.Get("/recap", onlyCourier, courierAppH.Recap) // rekap di tab Profil
	courierApp.Get("/orders", onlyCourier, courierAppH.Orders)
	courierApp.Get("/orders/:id", onlyCourier, courierAppH.Show)
	courierApp.Post("/orders/:id/status", onlyCourier, courierAppH.UpdateStatus)
	courierApp.Post("/orders/:id/deliver", onlyCourier, courierAppH.Deliver) // bukti terima (POD)
	courierApp.Get("/route", onlyCourier, courierAppH.MyRoute)               // rute yang sedang berjalan
	courierApp.Put("/route", onlyCourier, courierAppH.SaveRoute)             // urutan antar → dilihat Ops

	admin := func(roles ...enums.UserRole) fiber.Handler { return middleware.RequireRole(roles...) }

	// Order: lihat = semua staff; buat = CCO/Farmasi/Admin; tugaskan kurir = Ops/Admin;
	// ubah status = CCO/Farmasi/Admin sesuai tahapnya (kurir lewat /courier).
	orders := auth.Group("/orders")
	staff := []enums.UserRole{enums.RoleAdmin, enums.RoleCco, enums.RoleFarmasi, enums.RoleOps, enums.RoleFinance}
	orders.Get("/", admin(staff...), orderH.Index)
	orders.Get("/stats", admin(staff...), orderH.Stats)
	orders.Post("/labels", admin(staff...), orderH.Labels) // data lengkap untuk resi (tidak disamarkan)
	orders.Get("/:id", admin(staff...), orderH.Show)
	orders.Post("/", admin(enums.RoleAdmin, enums.RoleCco, enums.RoleFarmasi), orderH.Store)
	orders.Put("/:id", admin(enums.RoleAdmin, enums.RoleCco), orderH.Update)
	orders.Put("/:id/coordinates", admin(enums.RoleAdmin, enums.RoleCco, enums.RoleOps), orderH.Coordinates)
	// Which role may take which step is checked per order (enums.StaffCanMove).
	orders.Post("/bulk-transition", admin(enums.RoleAdmin, enums.RoleCco, enums.RoleFarmasi), orderH.BulkTransition)
	orders.Post("/bulk-assign-courier", admin(enums.RoleAdmin, enums.RoleOps), orderH.BulkAssignCourier)
	orders.Post("/:id/transition", admin(enums.RoleAdmin, enums.RoleCco, enums.RoleFarmasi), orderH.Transition)
	orders.Post("/:id/assign-courier", admin(enums.RoleAdmin, enums.RoleOps), orderH.AssignCourier)
	orders.Get("/:id/assignable-couriers", admin(enums.RoleAdmin, enums.RoleOps), orderH.AssignableCouriers)
	// Pembayaran: CCO tandai lunas (Finance/Admin juga), refund Finance/Admin.
	orders.Post("/:id/payment", admin(enums.RoleAdmin, enums.RoleFinance, enums.RoleCco), orderH.Payment)

	// Pengaturan pembayaran (rekening & QRIS): Admin
	settings := auth.Group("/settings", admin(enums.RoleAdmin))
	settings.Get("/payment", settingsH.Payment)
	settings.Put("/payment", settingsH.UpdatePayment)
	settings.Post("/payment/qris", settingsH.UploadQRIS)
	settings.Delete("/payment/qris", settingsH.DeleteQRIS)
	settings.Get("/setup-checklist", settingsH.SetupChecklist) // checklist setup awal di panel

	// Customer tersimpan: CCO/Farmasi + Admin
	customers := auth.Group("/customers", admin(enums.RoleAdmin, enums.RoleCco, enums.RoleFarmasi))
	customers.Get("/", custH.Index)
	customers.Post("/", custH.Store)
	customers.Put("/:id", custH.Update)
	customers.Delete("/:id", custH.Destroy)

	// Profil RS mitra: semua staff bisa lihat; Admin mengisi (sekali, saat setup) dan mengubah.
	auth.Get("/organization", orgH.Show)
	auth.Post("/organization", admin(enums.RoleAdmin), orgH.Setup)
	auth.Put("/organization", admin(enums.RoleAdmin), orgH.Update)
	auth.Put("/license", admin(enums.RoleAdmin), licenseH.Replace) // perpanjang / ganti lisensi

	// Factory reset (hapus SEMUA data, balik ke aktivasi lisensi): Admin, hanya kalau ALLOW_FACTORY_RESET=true.
	if allowFactoryReset {
		resetH := &handlers.ResetHandler{DB: db, Files: store}
		auth.Post("/factory-reset", admin(enums.RoleAdmin), resetH.FactoryReset)
	}

	// Service, User: Admin only

	svcs := auth.Group("/services", admin(enums.RoleAdmin))
	svcs.Get("/", svcH.Index)
	svcs.Post("/", svcH.Store)
	svcs.Put("/:id", svcH.Update)
	svcs.Delete("/:id", svcH.Destroy)

	users := auth.Group("/users", admin(enums.RoleAdmin))
	users.Get("/", userH.Index)
	users.Post("/", userH.Store)
	users.Put("/:id", userH.Update)

	auth.Get("/roles", admin(enums.RoleAdmin), roleH.Index)

	// Courier: Admin CRUD, Ops lihat aja
	couriers := auth.Group("/couriers")
	couriers.Get("/", admin(enums.RoleAdmin, enums.RoleOps), courierH.Index)
	couriers.Get("/recap", admin(enums.RoleAdmin, enums.RoleOps), courierH.Recap)
	couriers.Get("/routes", admin(enums.RoleAdmin, enums.RoleOps), routesH.Index) // peta "Rute kurir"
	couriers.Post("/", admin(enums.RoleAdmin), courierH.Store)
	couriers.Put("/:id", admin(enums.RoleAdmin), courierH.Update)

	// Rekap order harian/bulanan: semua staff
	reports := auth.Group("/reports", admin(staff...))
	reports.Get("/recap", reportH.Recap)
	reports.Get("/recap/export", reportH.RecapExport)

	// Transaction: Finance + Admin
	txns := auth.Group("/transactions", admin(enums.RoleAdmin, enums.RoleFinance))
	txns.Get("/", txnH.Index)
	txns.Get("/export", txnH.Export)
	txns.Patch("/:id", txnH.UpdatePayment)
}
