// Package storage keeps uploaded files (proof-of-delivery photos). Handlers
// only see the Storage interface: Local (disk, for development) or MinIO
// (production), chosen with STORAGE_DRIVER.
package storage

import (
	"context"
	"io"
)

type Storage interface {
	// Save stores r (size bytes) under key, e.g. "pod/2026/09/<code>-<random>.jpg".
	Save(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Delete(ctx context.Context, key string) error
	// URL is where a browser/app can load the file: a path on this API
	// ("/uploads/…") for local disk, a short-lived presigned link for MinIO.
	URL(key string) string
}
