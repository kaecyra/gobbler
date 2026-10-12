package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store is the remote object store a snapshot is uploaded to. It is sized to
// what Runner calls; S3Store is the implementation.
type Store interface {
	// Put uploads the file at path under key.
	Put(ctx context.Context, key, path string) error
	// List returns the keys that start with prefix.
	List(ctx context.Context, prefix string) ([]string, error)
	// Delete removes key.
	Delete(ctx context.Context, key string) error
}

// KeyPrefix normalises a configured S3 prefix: no leading slash and a single
// trailing one, or empty for the bucket root.
func KeyPrefix(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// Status is the outcome history the settings page and health check read.
type Status struct {
	// LastSuccess is when the last fully successful run started; zero if none
	// since start-up.
	LastSuccess time.Time
	// LastError is the last failed run's error, cleared by the next success.
	LastError string
}

// Result describes one run.
type Result struct {
	Path     string
	Uploaded bool
}

// Runner is the backup job: snapshot, upload, retention.
type Runner struct {
	DB  *sql.DB
	Dir string
	// Store is nil for local-only backups.
	Store Store
	// Prefix is the remote key prefix as returned by KeyPrefix.
	Prefix     string
	LocalKeep  int
	RemoteKeep int
	// Now defaults to time.Now.
	Now func() time.Time

	mu     sync.Mutex
	status Status
	// running serialises runs: the scheduler and a manual run share one job.
	running sync.Mutex
}

// Status returns the outcome of the runs so far.
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Run takes one snapshot, uploads it when a Store is set, then applies
// retention. Local retention runs whether or not the upload worked; remote
// retention runs only after a successful upload, so a broken endpoint can
// never shrink the remote history.
func (r *Runner) Run(ctx context.Context) (Result, error) {
	r.running.Lock()
	defer r.running.Unlock()

	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	started := now()
	res, err := r.run(ctx, started)

	r.mu.Lock()
	if err != nil {
		r.status.LastError = err.Error()
	} else {
		r.status = Status{LastSuccess: started}
	}
	r.mu.Unlock()

	if err != nil {
		slog.ErrorContext(ctx, "backup failed", "error", err.Error())
		return res, err
	}
	slog.InfoContext(ctx, "backup succeeded", "snapshot", filepath.Base(res.Path), "uploaded", res.Uploaded)
	return res, nil
}

func (r *Runner) run(ctx context.Context, now time.Time) (Result, error) {
	var res Result
	snap, err := Snapshot(ctx, r.DB, r.Dir, now)
	if err != nil {
		return res, fmt.Errorf("take snapshot: %w", err)
	}
	res.Path = snap

	var errs []error
	if r.Store != nil {
		if err := r.upload(ctx, snap); err != nil {
			errs = append(errs, err)
		} else {
			res.Uploaded = true
		}
	}
	if err := r.pruneLocal(); err != nil {
		errs = append(errs, err)
	}
	return res, errors.Join(errs...)
}

func (r *Runner) upload(ctx context.Context, snap string) error {
	key := r.Prefix + filepath.Base(snap)
	if err := r.Store.Put(ctx, key, snap); err != nil {
		return fmt.Errorf("upload %q: %w", key, err)
	}
	keys, err := r.Store.List(ctx, r.Prefix)
	if err != nil {
		return fmt.Errorf("list remote snapshots under %q: %w", r.Prefix, err)
	}
	byName := make(map[string]string, len(keys))
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		name := strings.TrimPrefix(k, r.Prefix)
		byName[name] = k
		names = append(names, name)
	}
	var errs []error
	for _, name := range Expired(names, r.RemoteKeep) {
		if err := r.Store.Delete(ctx, byName[name]); err != nil {
			errs = append(errs, fmt.Errorf("delete remote snapshot %q: %w", byName[name], err))
		}
	}
	return errors.Join(errs...)
}

func (r *Runner) pruneLocal() error {
	entries, err := readNames(r.Dir)
	if err != nil {
		return fmt.Errorf("list local snapshots: %w", err)
	}
	var errs []error
	for _, name := range Expired(entries, r.LocalKeep) {
		if err := removeIfExists(filepath.Join(r.Dir, name)); err != nil {
			errs = append(errs, fmt.Errorf("delete local snapshot: %w", err))
		}
	}
	return errors.Join(errs...)
}
