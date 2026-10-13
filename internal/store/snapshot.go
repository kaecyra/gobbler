package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/store/storedb"
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
// never modified (AGENTS.md boundary). That is a property of this API, not of
// the database (ADR-00004 obligation 4, ADR-00015): TestSnapshotAPIHasNoMutator
// fails if a method that changes or removes one appears here.

// AddSnapshot stores the ingested input of recipe id. Each kind is stored once
// per recipe; a second AddSnapshot of the same kind is ErrSnapshotExists and
// leaves the first untouched. A missing recipe is ErrNotFound.
func (s *Store) AddSnapshot(ctx context.Context, id int64, kind SnapshotKind, content []byte) error {
	who, err := identity(ctx)
	if err != nil {
		return err
	}
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	if !kind.Valid() {
		return fmt.Errorf("%w: snapshot kind %q", ErrInvalid, kind)
	}
	return s.withTx(ctx, func(q *storedb.Queries) error {
		if err := lockRecipe(ctx, q, hid, id); err != nil {
			return err
		}
		return s.insertSnapshot(ctx, q, hid, id, kind, content, who)
	})
}

// insertSnapshot writes one snapshot inside the transaction q is bound to. The
// caller holds the write lock on the recipe, which makes the existence check
// and the insert atomic, so no concurrent writer can slip a duplicate in
// between; the UNIQUE constraint is the backstop.
func (s *Store) insertSnapshot(ctx context.Context, q *storedb.Queries, hid, id int64, kind SnapshotKind, content []byte, who string) error {
	if !kind.Valid() {
		return fmt.Errorf("%w: snapshot kind %q", ErrInvalid, kind)
	}
	existing, err := q.CountSnapshots(ctx, storedb.CountSnapshotsParams{RecipeID: id, Kind: string(kind), HouseholdID: hid})
	if err != nil {
		return fmt.Errorf("check %s snapshot of recipe %d: %w", kind, id, err)
	}
	if existing > 0 {
		return fmt.Errorf("%w: recipe %d kind %s", ErrSnapshotExists, id, kind)
	}
	sum := sha256.Sum256(content)
	err = q.InsertSnapshot(ctx, storedb.InsertSnapshotParams{
		HouseholdID: hid, RecipeID: id, Kind: string(kind), Content: content, Sha256: hex.EncodeToString(sum[:]),
		CreatedAt: s.stamp(), CreatedBy: who,
	})
	if err != nil {
		return fmt.Errorf("write %s snapshot of recipe %d: %w", kind, id, err)
	}
	return nil
}

// GetSnapshot returns the stored input of the given kind, or ErrNotFound.
func (s *Store) GetSnapshot(ctx context.Context, id int64, kind SnapshotKind) (Snapshot, error) {
	hid, err := scope(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	row, err := s.q.GetSnapshot(ctx, storedb.GetSnapshotParams{RecipeID: id, Kind: string(kind), HouseholdID: hid})
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, fmt.Errorf("%w: %s snapshot of recipe %d", ErrNotFound, kind, id)
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read %s snapshot of recipe %d: %w", kind, id, err)
	}
	snap := Snapshot{Kind: SnapshotKind(row.Kind), Content: row.Content, SHA256: row.Sha256, CreatedBy: row.CreatedBy}
	if snap.CreatedAt, err = parseTime(row.CreatedAt); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// ListSnapshotKinds returns the kinds of snapshot stored for recipe id. A
// missing recipe is ErrNotFound.
func (s *Store) ListSnapshotKinds(ctx context.Context, id int64) ([]SnapshotKind, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	var out []SnapshotKind
	err = s.read(ctx, func(q *storedb.Queries) error {
		if _, err := loadInfo(ctx, q, hid, id); err != nil {
			return err
		}
		kinds, err := q.ListSnapshotKinds(ctx, storedb.ListSnapshotKindsParams{RecipeID: id, HouseholdID: hid})
		if err != nil {
			return readErr("snapshot kinds", err)
		}
		for _, k := range kinds {
			out = append(out, SnapshotKind(k))
		}
		return nil
	})
	return out, err
}
