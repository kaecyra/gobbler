// Package media keeps what people upload (ADR-00033): a storage interface with
// an S3-compatible and a local-directory implementation, and the image
// pipeline that turns an upload into a stripped, bounded WebP.
package media

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kaecyra/gobbler/internal/config"
)

// ErrNotFound is returned (wrapped) by Get when no object has the key.
var ErrNotFound = errors.New("media not found")

// Store reads and writes media objects by key. Everything that touches media
// goes through it (ADR-00033 obligation 1). Objects are small (an upload is at
// most MaxUploadBytes), so they pass as byte slices.
type Store interface {
	// Put stores data under key, replacing any object there.
	Put(ctx context.Context, key string, data []byte, contentType string) error
	// Get returns the object, or an error wrapping ErrNotFound.
	Get(ctx context.Context, key string) ([]byte, error)
	// Delete removes the object. A key with no object is not an error.
	Delete(ctx context.Context, key string) error
}

// LocalDirName is the directory under the data directory that holds media when
// S3 is not configured for it.
const LocalDirName = "media"

// New returns the store the configuration asks for: the S3 media bucket when
// S3 and a media bucket are both set, otherwise a directory under the data
// directory (ADR-00033).
func New(cfg config.Config) (Store, error) {
	if cfg.Backup.S3.Configured() && cfg.Backup.S3.MediaBucket != "" {
		return NewS3(cfg.Backup.S3, cfg.Backup.S3.MediaBucket)
	}
	return NewLocal(filepath.Join(cfg.Server.DataDir, LocalDirName)), nil
}

// validKey refuses keys that could leave the store's root or that no code here
// generates. Keys are slash-separated names of letters, digits and "-_.".
func validKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") {
		return fmt.Errorf("media key %q: want a relative name", key)
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("media key %q: bad path element %q", key, part)
		}
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '/':
		default:
			return fmt.Errorf("media key %q: character %q is not allowed", key, r)
		}
	}
	return nil
}
