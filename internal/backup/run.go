package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
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

// Where snapshots go, as reported in Status.Destination.
const (
	DestinationLocalOnly = "local only"
	DestinationRemote    = "local and remote"
)

// statusFile holds the persisted Status in the backup directory. Its name is
// not a snapshot name, so retention never touches it.
const statusFile = "status.json"

// Status is the outcome history the settings page and health check read.
type Status struct {
	// LastSuccess is when the last fully successful run started; zero if there
	// has been none. Persisted across restarts.
	LastSuccess time.Time `json:"last_success"`
	// LastError is the last failed run's error, cleared by the next success.
	// Persisted across restarts.
	LastError string `json:"last_error"`
	// Destination is DestinationLocalOnly when no remote store is configured.
	// It reflects the current configuration and is not persisted.
	Destination string `json:"-"`
}

// Result describes one run.
type Result struct {
	Path     string
	Size     int64 // snapshot size in bytes; zero if no snapshot was taken
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
	// loadOnce loads the persisted status from Dir on first use.
	loadOnce sync.Once
	// running serialises runs: the scheduler and a manual run share one job.
	running sync.Mutex
}

// Status returns the outcome of the runs so far, including those before the
// last restart.
func (r *Runner) Status() Status {
	r.loadOnce.Do(r.loadStatus)
	r.mu.Lock()
	defer r.mu.Unlock()
	st := r.status
	st.Destination = DestinationRemote
	if r.Store == nil {
		st.Destination = DestinationLocalOnly
	}
	return st
}

// loadStatus reads the persisted status. Status is informational, so an
// unreadable file degrades to an empty history instead of blocking backups,
// and the problem is logged.
func (r *Runner) loadStatus() {
	b, err := os.ReadFile(filepath.Join(r.Dir, statusFile))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err == nil {
		var st Status
		if err = json.Unmarshal(b, &st); err == nil {
			r.mu.Lock()
			r.status = st
			r.mu.Unlock()
			return
		}
	}
	slog.Error("load backup status; starting with an empty history", "dir", r.Dir, "error", err.Error())
}

// saveStatus writes st atomically (temporary file, then rename) so a crash
// never leaves a truncated status file.
func (r *Runner) saveStatus(st Status) error {
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("encode backup status: %w", err)
	}
	if err := os.MkdirAll(r.Dir, 0o750); err != nil {
		return fmt.Errorf("create backup directory %q: %w", r.Dir, err)
	}
	final := filepath.Join(r.Dir, statusFile)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write backup status: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return errors.Join(fmt.Errorf("finalise backup status: %w", err), removeIfExists(tmp))
	}
	return nil
}

// Run takes one snapshot, uploads it when a Store is set, then applies
// retention. Local retention runs whether or not the upload worked; remote
// retention runs only after a successful upload, so a broken endpoint can
// never shrink the remote history.
func (r *Runner) Run(ctx context.Context) (Result, error) {
	r.running.Lock()
	defer r.running.Unlock()
	r.loadOnce.Do(r.loadStatus)

	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	started := now()
	begin := time.Now()
	res, err := r.run(ctx, started)
	elapsed := time.Since(begin)

	r.mu.Lock()
	if err != nil {
		r.status.LastError = err.Error()
	} else {
		r.status.LastSuccess, r.status.LastError = started, ""
	}
	persist := r.status
	r.mu.Unlock()
	// The backup itself already succeeded or failed; failing to record that
	// must not turn a good snapshot into a reported failure, so it is logged.
	if serr := r.saveStatus(persist); serr != nil {
		slog.ErrorContext(ctx, "persist backup status", "error", serr.Error())
	}

	attrs := []any{"duration_ms", elapsed.Milliseconds(), "size_bytes", res.Size, "uploaded", res.Uploaded}
	if res.Path != "" {
		attrs = append(attrs, "snapshot", filepath.Base(res.Path))
	}
	if err != nil {
		slog.ErrorContext(ctx, "backup failed", append(attrs, "error", err.Error())...)
		return res, err
	}
	slog.InfoContext(ctx, "backup succeeded", attrs...)
	return res, nil
}

func (r *Runner) run(ctx context.Context, now time.Time) (Result, error) {
	var res Result
	snap, err := Snapshot(ctx, r.DB, r.Dir, now)
	if err != nil {
		return res, fmt.Errorf("take snapshot: %w", err)
	}
	res.Path = snap
	fi, err := os.Stat(snap)
	if err != nil {
		return res, fmt.Errorf("stat snapshot: %w", err)
	}
	res.Size = fi.Size()

	var errs []error
	if r.Store != nil {
		uploaded, err := r.upload(ctx, snap)
		res.Uploaded = uploaded
		if err != nil {
			errs = append(errs, err)
		}
	}
	if err := r.pruneLocal(); err != nil {
		errs = append(errs, err)
	}
	return res, errors.Join(errs...)
}

// upload puts snap in the store, then applies remote retention. uploaded
// reports whether the snapshot reached the store, even if retention failed
// afterwards.
func (r *Runner) upload(ctx context.Context, snap string) (uploaded bool, err error) {
	key := r.Prefix + filepath.Base(snap)
	if err := r.Store.Put(ctx, key, snap); err != nil {
		return false, fmt.Errorf("upload %q: %w", key, err)
	}
	keys, err := r.Store.List(ctx, r.Prefix)
	if err != nil {
		return true, fmt.Errorf("list remote snapshots under %q: %w", r.Prefix, err)
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
	return true, errors.Join(errs...)
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
