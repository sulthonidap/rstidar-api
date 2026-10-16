// cmd/migrate menjalankan migrasi SQL di folder migrations/ ke database
// yang dikonfigurasi lewat .env. Pemakaian:
//
//	go run ./cmd/migrate up
//	go run ./cmd/migrate down
//	go run ./cmd/migrate drop
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"medsend-api/internal/config"
)

func main() {
	cfg := config.Load()

	dsn := fmt.Sprintf(
		"mysql://%s:%s@tcp(%s:%s)/%s?multiStatements=true",
		cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName,
	)

	m, err := migrate.New("file://migrations", dsn)
	if err != nil {
		log.Fatalf("migrate init: %v", err)
	}

	action := "up"
	if len(os.Args) > 1 {
		action = os.Args[1]
	}

	switch action {
	case "up":
		err = m.Up()
	case "down":
		err = m.Steps(-1)
	case "drop":
		err = m.Drop()
	default:
		log.Fatalf("aksi tidak dikenal: %s (pakai up/down/drop)", action)
	}

	if err != nil && err != migrate.ErrNoChange {
		log.Fatalf("migrate %s: %v", action, err)
	}

	log.Printf("migrate %s: selesai", action)
}
