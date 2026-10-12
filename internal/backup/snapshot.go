package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"
)

const (
	namePrefix = "gobbler-"
	nameSuffix = ".db"
	nameLayout = "20060102T150405Z"
)

var snapshotName = regexp.MustCompile(`^gobbler-\d{8}T\d{6}Z\.db$`)

// SnapshotName is the file name of the snapshot taken at t. Names sort
// chronologically as strings.
func SnapshotName(t time.Time) string {
	return namePrefix + t.UTC().Format(nameLayout) + nameSuffix
}

// IsSnapshotName reports whether name is a bare snapshot file name. Retention
// touches only names that pass, so a stray file in the backup directory or
// bucket is never deleted.
func IsSnapshotName(name string) bool { return snapshotName.MatchString(name) }

// Expired returns the snapshot names in names that fall outside the newest
// keep. Names that are not snapshot names are ignored. A keep below one
// deletes nothing: retention must never be able to empty the set.
func Expired(names []string, keep int) []string {
	if keep < 1 {
		return nil
	}
	snaps := slices.DeleteFunc(slices.Clone(names), func(n string) bool { return !IsSnapshotName(n) })
	slices.Sort(snaps)
	if len(snaps) <= keep {
		return nil
	}
	return snaps[:len(snaps)-keep]
}

// Snapshot writes a consistent copy of d into dir with VACUUM INTO and returns
// its path. The copy is built under a temporary name and renamed only once
// complete, so a failed run leaves no file that looks like a backup. An
// existing snapshot of the same name is never overwritten.
func Snapshot(ctx context.Context, d *sql.DB, dir string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create backup directory %q: %w", dir, err)
	}
	final := filepath.Join(dir, SnapshotName(now))
	if _, err := os.Lstat(final); err == nil {
		return "", fmt.Errorf("snapshot %q already exists", final)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check snapshot %q: %w", final, err)
	}

	partial := final + ".partial"
	// A crash can leave a partial behind, and VACUUM INTO refuses a non-empty target.
	if err := os.Remove(partial); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("remove stale partial snapshot %q: %w", partial, err)
	}
	if _, err := d.ExecContext(ctx, `VACUUM INTO ?`, partial); err != nil {
		return "", errors.Join(fmt.Errorf("vacuum into %q: %w", partial, err), removeIfExists(partial))
	}
	if err := os.Rename(partial, final); err != nil {
		return "", errors.Join(fmt.Errorf("finalise snapshot %q: %w", final, err), removeIfExists(partial))
	}
	return final, nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %q: %w", path, err)
	}
	return nil
}
