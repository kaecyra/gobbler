// Package catalog is the store for the canonical ingredient catalog, units and
// equipment (ADR-00008, ADR-00005). It owns the SQL for those tables, matching
// of free text to a canonical entry, and the transactional merge of two
// ingredients. The schema and the curated seed arrive through the migration
// in internal/db/migrations; the seed data itself lives in internal/catalog/seed.
//
// Nothing here creates a catalog entry as a side effect of matching: a match
// that finds nothing returns false, and creation is always an explicit call
// (ADR-00008 obligation 3).
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// ErrNotFound is returned when an entry looked up by id does not exist.
var ErrNotFound = errors.New("catalog entry not found")

// ErrConflict is returned when a name or alias is already taken by another
// entry of the same kind.
var ErrConflict = errors.New("catalog name or alias already in use")

// ErrInvalid is returned when an entry fails validation before it is stored.
var ErrInvalid = errors.New("invalid catalog entry")

// Store reads and writes the catalog. It is safe for concurrent use.
type Store struct {
	db *sql.DB

	// mergeHooks run inside the ingredient merge transaction. See
	// OnIngredientMerge.
	mergeHooks []MergeHook
}

// New returns a Store over d, which must already be migrated.
func New(d *sql.DB) *Store { return &Store{db: d} }

// queryer is the read surface shared by *sql.DB and *sql.Tx.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// execer adds writes; *sql.Tx satisfies it.
type execer interface {
	queryer
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// normalize is the stored and compared form of a name or alias: trimmed,
// lower-case, runs of whitespace collapsed to one space.
func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// normalizeAliases normalizes, drops empties and duplicates, and rejects an
// alias equal to the entry's own name.
func normalizeAliases(name string, aliases []string) []string {
	seen := map[string]bool{name: true}
	var out []string
	for _, a := range aliases {
		a = normalize(a)
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// validateName rejects names that are empty or carry a qualifier: ingredient
// names exclude preparation ("melted butter" is "butter" plus "melted").
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalid)
	}
	if strings.ContainsAny(name, ",()") {
		return fmt.Errorf("%w: name %q carries a qualifier; store the bare name", ErrInvalid, name)
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// withTx runs fn in a transaction, rolling back on error.
func (s *Store) withTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, rollback(tx))
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func rollback(tx *sql.Tx) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("rollback: %w", err)
	}
	return nil
}

// wrapWrite maps a UNIQUE or PRIMARY KEY violation to ErrConflict and wraps
// anything else, including CHECK, NOT NULL and foreign key failures, as it
// is. The driver reports the SQLite result code, so no message text is read.
func wrapWrite(what string, err error) error {
	var se *sqlite.Error
	if errors.As(err, &se) && (se.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE || se.Code() == sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY) {
		return fmt.Errorf("%w: %s: %w", ErrConflict, what, err)
	}
	return fmt.Errorf("%s: %w", what, err)
}
