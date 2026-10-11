package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// ErrSnapshotExists is returned when a recipe already has a source snapshot of
// that kind. A snapshot is stored once and there is no way to replace it.
var ErrSnapshotExists = errors.New("source snapshot already stored")

// SnapshotKind says what the ingested input was (ADR-00004).
type SnapshotKind string

// Snapshot kinds.
const (
	SnapshotHTML        SnapshotKind = "html"
	SnapshotText        SnapshotKind = "text"
	SnapshotOCR         SnapshotKind = "ocr"
	SnapshotLLMResponse SnapshotKind = "llm_response"
)

// Valid reports whether k is one of the defined kinds.
func (k SnapshotKind) Valid() bool {
	switch k {
	case SnapshotHTML, SnapshotText, SnapshotOCR, SnapshotLLMResponse:
		return true
	}
	return false
}

// Snapshot is the ingested input of a recipe, byte for byte as it arrived.
type Snapshot struct {
	Kind      SnapshotKind
	Content   []byte
	SHA256    string
	CreatedAt time.Time
	CreatedBy string
}

// The store exposes snapshots for add and read only. Source snapshots are
// never modified (AGENTS.md boundary); the migration's triggers refuse an
// UPDATE, a stand-alone DELETE and an insert over an existing kind
// (INSERT OR REPLACE), and TestSnapshotAPIHasNoMutator fails if
// a method that changes or removes one appears here.

// AddSnapshot stores the ingested input of recipe id. Each kind is stored once
// per recipe; a second AddSnapshot of the same kind is ErrSnapshotExists and
// leaves the first untouched. A missing recipe is ErrNotFound.
func (s *Store) AddSnapshot(ctx context.Context, id int64, kind SnapshotKind, content []byte) error {
	who, err := identity(ctx)
	if err != nil {
		return err
	}
	if !kind.Valid() {
		return fmt.Errorf("%w: snapshot kind %q", ErrInvalid, kind)
	}
	sum := sha256.Sum256(content)
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := lockRecipe(ctx, tx, id); err != nil {
			return err
		}
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM source_snapshots WHERE recipe_id = ? AND kind = ?`, id, string(kind)).Scan(&existing); err != nil {
			return fmt.Errorf("check %s snapshot of recipe %d: %w", kind, id, err)
		}
		if existing > 0 {
			return fmt.Errorf("%w: recipe %d kind %s", ErrSnapshotExists, id, kind)
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO source_snapshots (recipe_id, kind, content, sha256, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
			id, string(kind), content, hex.EncodeToString(sum[:]), s.stamp(), who)
		var se *sqlite.Error
		if errors.As(err, &se) && se.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE {
			return fmt.Errorf("%w: recipe %d kind %s", ErrSnapshotExists, id, kind)
		}
		if err != nil {
			return fmt.Errorf("write %s snapshot of recipe %d: %w", kind, id, err)
		}
		return nil
	})
}

// GetSnapshot returns the stored input of the given kind, or ErrNotFound.
func (s *Store) GetSnapshot(ctx context.Context, id int64, kind SnapshotKind) (Snapshot, error) {
	var (
		snap Snapshot
		at   string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT kind, content, sha256, created_at, created_by FROM source_snapshots WHERE recipe_id = ? AND kind = ?`, id, string(kind)).
		Scan(&snap.Kind, &snap.Content, &snap.SHA256, &at, &snap.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, fmt.Errorf("%w: %s snapshot of recipe %d", ErrNotFound, kind, id)
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read %s snapshot of recipe %d: %w", kind, id, err)
	}
	if snap.CreatedAt, err = parseTime(at); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// ListSnapshotKinds returns the kinds of snapshot stored for recipe id. A
// missing recipe is ErrNotFound.
func (s *Store) ListSnapshotKinds(ctx context.Context, id int64) ([]SnapshotKind, error) {
	if _, err := loadInfo(ctx, s.db, id); err != nil {
		return nil, err
	}
	var out []SnapshotKind
	err := collect(ctx, s.db, "snapshot kinds", `SELECT kind FROM source_snapshots WHERE recipe_id = ? ORDER BY id`, []any{id}, func(rows *sql.Rows) error {
		var k SnapshotKind
		if err := rows.Scan(&k); err != nil {
			return err
		}
		out = append(out, k)
		return nil
	})
	return out, err
}
