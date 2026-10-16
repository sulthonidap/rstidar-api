package config

import (
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv    string
	AppPort   string
	DBHost    string
	DBPort    string
	DBName    string
	DBUser    string
	DBPass    string
	JWTSecret string
	JWTTTL    time.Duration
	// Uploaded files (POD photos): "minio" (default) or "local" disk (offline testing only).
	StorageDriver  string
	UploadDir      string // local driver only
	MinIOEndpoint  string
	MinIOUseSSL    bool
	MinIOPublicURL string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOBucket    string
	MinIORegion    string
	// OSRM-compatible routing server for road distances (empty = straight-line estimate).
	RoutingURL string
	// Enables POST /api/factory-reset (wipes ALL data). Demo servers only.
	AllowFactoryReset bool
}

func Load() *Config {
	_ = godotenv.Load()

	return &Config{
		AppEnv:         getenv("APP_ENV", "local"),
		AppPort:        getenv("APP_PORT", "8080"),
		DBHost:         getenv("DB_HOST", "127.0.0.1"),
		DBPort:         getenv("DB_PORT", "3306"),
		DBName:         getenv("DB_NAME", "medsend_go"),
		DBUser:         getenv("DB_USER", "root"),
		DBPass:         getenv("DB_PASSWORD", ""),
		JWTSecret:      getenv("JWT_SECRET", "dev-secret-change-me"),
		JWTTTL:         24 * time.Hour,
		StorageDriver:  getenv("STORAGE_DRIVER", "minio"),
		UploadDir:      getenv("UPLOAD_DIR", "./storage/uploads"),
		MinIOEndpoint:  os.Getenv("MINIO_ENDPOINT"),
		MinIOUseSSL:    getenv("MINIO_USE_SSL", "true") == "true",
		MinIOPublicURL: os.Getenv("MINIO_PUBLIC_URL"),
		MinIOAccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		MinIOSecretKey: os.Getenv("MINIO_SECRET_KEY"),
		MinIOBucket:    getenv("MINIO_BUCKET", "medsent"),
		MinIORegion:    getenv("MINIO_REGION", "us-east-1"),
		RoutingURL:     os.Getenv("ROUTING_URL"),

		AllowFactoryReset: os.Getenv("ALLOW_FACTORY_RESET") == "true",
	}
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
