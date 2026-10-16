package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Local stores files under Root and serves them at URLPrefix (see main.go).
// In a container, Root must be on a mounted volume or files vanish on redeploy.
type Local struct {
	Root      string
	URLPrefix string // e.g. "/uploads"
}

func (l *Local) path(key string) (string, error) {
	clean := path.Clean("/" + key)
	if strings.Contains(key, "..") || clean == "/" {
		return "", errors.New("key tidak valid")
	}
	return filepath.Join(l.Root, filepath.FromSlash(clean)), nil
}

func (l *Local) Save(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(p)
		return err
	}
	return f.Close()
}

func (l *Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (l *Local) URL(key string) string {
	return strings.TrimSuffix(l.URLPrefix, "/") + "/" + strings.TrimPrefix(key, "/")
}
