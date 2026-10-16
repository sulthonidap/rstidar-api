package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"

	"medsend-api/internal/config"
	"medsend-api/internal/database"
	"medsend-api/internal/routes"
	"medsend-api/internal/routing"
	"medsend-api/internal/storage"
)

// newStorage picks where uploaded files (POD photos) go: MinIO in production,
// or local disk served at /uploads (random file names, no directory listing).
func newStorage(cfg *config.Config, app *fiber.App) (storage.Storage, error) {
	switch cfg.StorageDriver {
	case "minio":
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		store, err := storage.NewMinIO(ctx, storage.MinIOConfig{
			Endpoint: cfg.MinIOEndpoint, UseSSL: cfg.MinIOUseSSL, PublicURL: cfg.MinIOPublicURL,
			AccessKey: cfg.MinIOAccessKey, SecretKey: cfg.MinIOSecretKey,
			Bucket: cfg.MinIOBucket, Region: cfg.MinIORegion,
		})
		if err != nil {
			return nil, err
		}
		log.Printf("storage: MinIO bucket %q", cfg.MinIOBucket)
		return store, nil
	case "local":
		app.Static("/uploads", cfg.UploadDir, fiber.Static{Browse: false, MaxAge: 7 * 24 * 3600})
		log.Printf("storage: disk lokal %s", cfg.UploadDir)
		return &storage.Local{Root: cfg.UploadDir, URLPrefix: "/uploads"}, nil
	}
	return nil, fmt.Errorf("STORAGE_DRIVER tidak dikenal: %q (pakai local/minio)", cfg.StorageDriver)
}

func main() {
	cfg := config.Load()

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}

	if err := database.Migrate(cfg); err != nil {
		log.Fatalf("db migrate: %v", err)
	}
	if err := database.Bootstrap(db); err != nil {
		log.Fatalf("db bootstrap: %v", err)
	}

	app := fiber.New(fiber.Config{
		// Proof-of-delivery uploads: up to 3 photos of max 8 MB each.
		BodyLimit: 30 << 20,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if fe, ok := err.(*fiber.Error); ok {
				code = fe.Code
			}
			return c.Status(code).JSON(fiber.Map{"error": err.Error()})
		},
	})

	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		AllowMethods: "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		// Lets the web app (another origin) read the file name of downloads like the Excel export.
		ExposeHeaders: "Content-Disposition",
	}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	store, err := newStorage(cfg, app)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	var router routing.Router
	if cfg.RoutingURL != "" {
		router = routing.NewOSRM(cfg.RoutingURL)
		log.Printf("routing: %s", cfg.RoutingURL)
		if strings.Contains(cfg.RoutingURL, "router.project-osrm.org") && cfg.AppEnv == "production" {
			log.Printf("routing: PERINGATAN — server demo OSRM tidak untuk produksi, pakai OSRM sendiri")
		}
	} else {
		log.Printf("routing: ROUTING_URL kosong, jarak = perkiraan garis lurus × 1,3")
	}

	routes.Register(app, db, store, router, cfg.JWTSecret, cfg.JWTTTL, cfg.AllowFactoryReset)
	if cfg.AllowFactoryReset {
		log.Printf("PERINGATAN: ALLOW_FACTORY_RESET=true — admin bisa menghapus SEMUA data lewat POST /api/factory-reset")
	}

	log.Printf("MedSend API listening on :%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	log.Fatal(app.Listen(":" + cfg.AppPort))
}
