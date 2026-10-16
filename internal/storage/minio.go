package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOConfig — see .env.example (STORAGE_DRIVER=minio).
type MinIOConfig struct {
	Endpoint  string // "https://s3.example.com" or "minio:9000" (then UseSSL decides the scheme)
	UseSSL    bool
	PublicURL string // where browsers reach MinIO, if different from Endpoint (e.g. internal docker host)
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	URLTTL    time.Duration // how long a photo link stays valid
}

// MinIO stores files in a private bucket; URL hands out presigned links, so
// photos are only reachable through a link the API just gave out.
type MinIO struct {
	client *minio.Client // uploads/deletes (may use the internal endpoint)
	signer *minio.Client // presigned links (public endpoint; signing makes no network call)
	bucket string
	ttl    time.Duration
}

// endpointHost accepts "https://host", "http://host:9000" or "host:9000".
func endpointHost(raw string, defaultSSL bool) (host string, secure bool, err error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		return strings.TrimSuffix(raw, "/"), defaultSSL, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", false, fmt.Errorf("endpoint MinIO tidak valid: %q", raw)
	}
	return u.Host, u.Scheme == "https", nil
}

func NewMinIO(ctx context.Context, cfg MinIOConfig) (*MinIO, error) {
	if cfg.Endpoint == "" || cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("MINIO_ENDPOINT, MINIO_ACCESS_KEY, MINIO_SECRET_KEY dan MINIO_BUCKET wajib diisi")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	if cfg.URLTTL <= 0 {
		cfg.URLTTL = time.Hour
	}

	newClient := func(endpoint string, defaultSSL bool) (*minio.Client, error) {
		host, secure, err := endpointHost(endpoint, defaultSSL)
		if err != nil {
			return nil, err
		}
		// A fixed region avoids a bucket-location request before every call.
		return minio.New(host, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: secure,
			Region: cfg.Region,
		})
	}

	client, err := newClient(cfg.Endpoint, cfg.UseSSL)
	if err != nil {
		return nil, err
	}
	signer := client
	if cfg.PublicURL != "" {
		if signer, err = newClient(cfg.PublicURL, true); err != nil {
			return nil, err
		}
	}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("tidak bisa terhubung ke MinIO: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("bucket %q belum ada dan gagal dibuat: %w", cfg.Bucket, err)
		}
		log.Printf("storage: bucket MinIO %q dibuat", cfg.Bucket)
	}

	return &MinIO{client: client, signer: signer, bucket: cfg.Bucket, ttl: cfg.URLTTL}, nil
}

func (m *MinIO) Save(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (m *MinIO) Delete(ctx context.Context, key string) error {
	return m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{})
}

func (m *MinIO) URL(key string) string {
	u, err := m.signer.PresignedGetObject(context.Background(), m.bucket, key, m.ttl, nil)
	if err != nil {
		log.Printf("storage: presign %s: %v", key, err)
		return ""
	}
	return u.String()
}
