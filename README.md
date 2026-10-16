# MedSend API

Backend MedSend: **Go + Fiber + GORM + MySQL/MariaDB**. Melayani satu RS mitra
(profilnya diisi admin saat pertama login), panel staf (React), form order
publik, dan aplikasi kurir.

## Setup

```bash
cp .env.example .env        # isi DB_*, JWT_SECRET
go run .                    # server API di :8080 — migrasi database jalan otomatis saat start
```

Saat aplikasi pertama kali dibuka, panel memandu tiga langkah:

1. **Aktivasi Lisensi** — tempel kode lisensi `MSND-...` dari penyedia aplikasi.
   Sebelum diaktivasi, tidak ada yang bisa login.
2. **Buat Akun Admin** — akun admin pertama (user lain ditambah dari Kelola User).
3. **Profil RS** — alamat, titik lokasi, wilayah layanan. Nama RS mengikuti lisensi.

Lisensi diverifikasi offline (tanda tangan Ed25519, `internal/license`). Lisensi
yang lewat masa berlaku hanya memunculkan peringatan ke admin; lisensi baru
dimasukkan di Profil RS.

Data contoh untuk development (opsional):

```bash
go run ./cmd/seed           # role, admin, RS contoh, staf, kurir, layanan, order contoh
```

Login seed: `admin@medsend.test` / `password` (semua akun seed sama). Seed tidak
membuat lisensi: aktivasi dulu lewat panel sebelum login.

## Struktur

```
main.go      entrypoint HTTP server
cmd/
  migrate/   jalanin migrations/*.sql manual (golang-migrate)
  seed/      data contoh
internal/
  config/    baca .env
  database/  koneksi, migrasi otomatis, bootstrap role + admin
  enums/     OrderStatus (+ aturan transisi), OrderSource, PaymentStatus, UserRole
  models/    struct GORM
  services/  OrderService (transisi status, assign kurir, transaksi), tarif jarak
  routing/   jarak lewat jalan (OSRM)
  realtime/  notifikasi order live ke panel (Server-Sent Events)
  storage/   file upload (MinIO / disk lokal)
  auth/      JWT
  middleware/RequireAuth, RequireRole, RequireCourier
  handlers/  HTTP handler per resource
  routes/    daftar route + role guard
migrations/  SQL up/down, ikut ter-embed ke binary
```

## Catatan

- **Satu RS mitra.** Semua order, customer, dan transaksi milik RS itu
  (`organizations`, satu baris). Ambil lewat `partnerRS()` di
  `handlers/organization_handler.go`; endpoint `GET/POST/PUT /api/organization`.
- **Lisensi.** Disimpan di `app_settings` (`license.key`); alur pertama kali di
  `handlers/license_handler.go` (`/api/public/setup`, `/activate`, `/setup-admin`,
  `PUT /api/license`).
- **Wilayah layanan.** `organizations.max_distance_km` membatasi jarak (lewat
  jalan) RS → tujuan; order di luar itu ditolak. Kosong = tanpa batas.
- **Tarif.** Per layanan: harga flat, atau tier jarak + tarif per km setelah
  tier terakhir (`services/pricing.go`).
- **Auth JWT (Bearer token)** untuk panel SPA dan aplikasi kurir.

## Test

```bash
go test ./...
```

Test memakai SQLite in-memory (`glebarez/sqlite`, pure Go, tanpa cgo).
