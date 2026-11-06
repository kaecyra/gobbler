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
// that kind and a caller asked to add another. Nothing replaces a snapshot; the
// only way a recipe gains a second of a kind is a saved re-import
// (AcceptReimport, ADR-00042).
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
	// ParserVersion is the ingest parser version that produced the recipe
	// this snapshot was taken for; empty when unknown (ADR-00006).
	ParserVersion string
}

// The store exposes snapshots for add and read only. Source snapshots are
// never modified (AGENTS.md boundary). That is a property of this API, not of
// the database (ADR-00004 obligation 4, ADR-00015): TestSnapshotAPIHasNoMutator
// fails if a method that changes or removes one appears here. The one other
// thing the store does with a snapshot is a draft's (ADR-00035, proposal.go): it
// writes one with no recipe, attaches it once to the recipe the draft becomes,
// and deletes one no recipe holds. None of that changes content, and none is
// exported as a snapshot operation.

// AddSnapshot stores the ingested input of recipe id. It adds a kind once; a
// second AddSnapshot of the same kind is ErrSnapshotExists and
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

// insertSnapshot writes one snapshot with no recorded parser version: what
// AddSnapshot stores.
func (s *Store) insertSnapshot(ctx context.Context, q *storedb.Queries, hid, id int64, kind SnapshotKind, content []byte, who string) error {
	return s.insertVersionedSnapshot(ctx, q, hid, id, SnapshotInput{Kind: kind, Content: content}, who)
}

// insertVersionedSnapshot writes one snapshot inside the transaction q is
// bound to. The caller holds the write lock on the recipe, which makes the
// existence check and the insert atomic, so no concurrent writer can slip a
// duplicate in between. Since migration 00032 nothing in the schema backs the
// check up: this is the only guard for a kind given twice. A caller that may
// legitimately add a second snapshot of a kind (a share copying a re-imported
// recipe) uses writeSnapshot.
func (s *Store) insertVersionedSnapshot(ctx context.Context, q *storedb.Queries, hid, id int64, in SnapshotInput, who string) error {
	kind := in.Kind
	if !kind.Valid() {
		return fmt.Errorf("%w: snapshot kind %q", ErrInvalid, kind)
	}
	existing, err := q.CountSnapshots(ctx, storedb.CountSnapshotsParams{RecipeID: nullID(id), Kind: string(kind), HouseholdID: hid})
	if err != nil {
		return fmt.Errorf("check %s snapshot of recipe %d: %w", kind, id, err)
	}
	if existing > 0 {
		return fmt.Errorf("%w: recipe %d kind %s", ErrSnapshotExists, id, kind)
	}
	return s.writeSnapshot(ctx, q, hid, id, in, who)
}

// writeSnapshot hashes and stores one snapshot of recipe id with no check for
// another of its kind.
func (s *Store) writeSnapshot(ctx context.Context, q *storedb.Queries, hid, id int64, in SnapshotInput, who string) error {
	if !in.Kind.Valid() {
		return fmt.Errorf("%w: snapshot kind %q", ErrInvalid, in.Kind)
	}
	sum := sha256.Sum256(in.Content)
	err := q.InsertSnapshot(ctx, storedb.InsertSnapshotParams{
		HouseholdID: hid, RecipeID: nullID(id), Kind: string(in.Kind), Content: in.Content, Sha256: hex.EncodeToString(sum[:]),
		CreatedAt: s.stamp(), CreatedBy: who, ParserVersion: in.ParserVersion,
	})
	if err != nil {
		return fmt.Errorf("write %s snapshot of recipe %d: %w", in.Kind, id, err)
	}
	return nil
}

// GetSnapshot returns the stored input of the given kind, or ErrNotFound.
func (s *Store) GetSnapshot(ctx context.Context, id int64, kind SnapshotKind) (Snapshot, error) {
	hid, err := scope(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	row, err := s.q.GetSnapshot(ctx, storedb.GetSnapshotParams{RecipeID: nullID(id), Kind: string(kind), HouseholdID: hid})
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, fmt.Errorf("%w: %s snapshot of recipe %d", ErrNotFound, kind, id)
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("read %s snapshot of recipe %d: %w", kind, id, err)
	}
	snap := Snapshot{Kind: SnapshotKind(row.Kind), Content: row.Content, SHA256: row.Sha256, CreatedBy: row.CreatedBy, ParserVersion: row.ParserVersion}
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
		kinds, err := q.ListSnapshotKinds(ctx, storedb.ListSnapshotKindsParams{RecipeID: nullID(id), HouseholdID: hid})
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
