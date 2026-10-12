// Package store persists recipes (ADR-00004): the recipe document with its
// components, ingredient lines, steps and dependency graph, plus notes, made-it
// entries, revisions, clones and source snapshots. It owns the SQL
// for those tables; the schema arrives through the migrations in
// internal/db/migrations. The SQL is written in queries/ and generated into
// storedb by sqlc (ADR-00014).
//
// Every write records the verified identity found on the request context
// (ADR-00003) and fails with ErrNoIdentity when there is none; data is not
// partitioned by user. Every edit writes a revision of the document it
// replaces in the same transaction.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// ErrNotFound is returned when a recipe (or a version of it) does not exist.
// It is distinct from a failed query, which is returned wrapped as it is.
var ErrNotFound = errors.New("not found")

// ErrInvalid is returned when input fails a rule the store enforces itself,
// such as an empty note. Recipe document rules are the recipe package's errors.
var ErrInvalid = errors.New("invalid input")

// ErrUnknownReference is returned when a document names a unit, ingredient or
// equipment entry that is not in the catalog.
var ErrUnknownReference = errors.New("reference to an unknown catalog entry")

// ErrNoIdentity is returned by a write whose context carries no verified
// identity. Writes are never anonymous.
var ErrNoIdentity = errors.New("no verified identity on the request context")

// ErrCorrupt is returned when stored rows disagree with each other, such as a
// step dependency naming a step that does not exist. It means the database is
// broken, not that the caller asked for something absent.
var ErrCorrupt = errors.New("stored data is inconsistent")

// lookup returns m[key], or an ErrCorrupt error naming what was missing.
func lookup[K comparable, V any](m map[K]V, key K, what string) (V, error) {
	v, ok := m[key]
	if !ok {
		return v, fmt.Errorf("%w: %s %v not found", ErrCorrupt, what, key)
	}
	return v, nil
}

// Store reads and writes recipes. It is safe for concurrent use.
type Store struct {
	db  *sql.DB
	q   *storedb.Queries
	now func() time.Time
}

// New returns a Store over d, which must already be migrated.
func New(d *sql.DB) *Store { return &Store{db: d, q: storedb.New(d), now: time.Now} }

// identity returns the verified user for a write.
func identity(ctx context.Context) (string, error) {
	email, ok := auth.EmailFromContext(ctx)
	if !ok {
		return "", ErrNoIdentity
	}
	return email, nil
}

// stamp returns the current time in the stored form.
func (s *Store) stamp() string { return formatTime(s.now()) }

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time %q: %w", s, err)
	}
	return t, nil
}

// withTx runs fn in one transaction (db.WithTx). A caller that reads before it
// writes must make its first statement a write, such as lockRecipe, so the
// transaction takes the database write lock up front instead of failing to
// upgrade a stale read snapshot: writers outside the db.WithTx queue (other
// processes, bare-pool writes) can still commit between its read and write.
func (s *Store) withTx(ctx context.Context, fn func(q *storedb.Queries) error) error {
	return db.WithTx(ctx, s.db, s.q, func(q *storedb.Queries, _ *sql.Tx) error { return fn(q) })
}

// read runs fn against one read-only transaction (db.WithReadTx), so every
// query in a multi-query read sees the same snapshot.
func (s *Store) read(ctx context.Context, fn func(q *storedb.Queries) error) error {
	return db.WithReadTx(ctx, s.db, s.q, func(q *storedb.Queries, _ *sql.Tx) error { return fn(q) })
}

// wrapWrite wraps a failed write with what it was doing. A foreign key
// violation is a document naming a catalog entry that does not exist, which
// callers branch on as ErrUnknownReference.
func wrapWrite(what string, err error) error {
	return db.WrapWrite(what, err, ErrUnknownReference, db.ConstraintForeignKey)
}

// readErr wraps a failed read with what it was reading.
func readErr(what string, err error) error {
	return fmt.Errorf("read %s: %w", what, err)
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
