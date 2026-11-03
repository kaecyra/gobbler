package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Local is a Store over a directory on disk: the fallback when no S3 media
// bucket is configured (ADR-00033).
type Local struct{ root string }

// NewLocal returns a Local rooted at dir. The directory is created by the first
// Put, so building one touches no disk.
func NewLocal(dir string) *Local { return &Local{root: dir} }

func (l *Local) path(key string) (string, error) {
	if err := validKey(key); err != nil {
		return "", err
	}
	return filepath.Join(l.root, filepath.FromSlash(key)), nil
}

// Put writes data to a temporary file and renames it into place, so a reader
// never sees half an object.
func (l *Local) Put(ctx context.Context, key string, data []byte, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("create directory for media %q: %w", key, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".put-*")
	if err != nil {
		return fmt.Errorf("write media %q: %w", key, err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name()) // the write already failed; this is cleanup
		return fmt.Errorf("write media %q: %w", key, err)
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		_ = os.Remove(tmp.Name()) // the rename already failed; this is cleanup
		return fmt.Errorf("write media %q: %w", key, err)
	}
	return nil
}

// Get reads the object.
func (l *Local) Get(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p, err := l.path(key)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("media %q: %w", key, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("read media %q: %w", key, err)
	}
	return b, nil
}

// Delete removes the object; a missing one is fine.
func (l *Local) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("delete media %q: %w", key, err)
	}
	return nil
}
