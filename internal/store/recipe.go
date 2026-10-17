package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// Origin names the recipe and version a clone was copied from.
type Origin struct {
	RecipeID int64
	Version  int64
}

// Info is the bookkeeping of a stored recipe, kept apart from its document.
type Info struct {
	ID int64
	// Version is 1 on create and grows by one on every edit or restore.
	Version              int64
	CreatedAt, UpdatedAt time.Time
	CreatedBy, UpdatedBy string
	// ClonedFrom is set for a clone whose origin recipe still exists.
	ClonedFrom *Origin
}

// Create stores r as a new recipe at version 1 and returns its id. r is
// validated first (recipe.Recipe.Validate); an invalid document writes nothing.
// Creating is not an edit and writes no revision.
func (s *Store) Create(ctx context.Context, r recipe.Recipe) (int64, error) {
	return s.create(ctx, r, nil)
}

// SnapshotInput is one source snapshot to store with a new recipe.
type SnapshotInput struct {
	Kind    SnapshotKind
	Content []byte
	// ParserVersion is the ingest parser version that produced the recipe;
	// empty when unknown.
	ParserVersion string
}

// CreateWithSnapshots is Create plus the source snapshots of what the recipe
// was made from, all in one transaction: either the recipe and every snapshot
// are stored, or nothing is. It fails like AddSnapshot for an invalid kind
// (ErrInvalid) and for a kind given twice (ErrSnapshotExists).
func (s *Store) CreateWithSnapshots(ctx context.Context, r recipe.Recipe, snaps ...SnapshotInput) (int64, error) {
	return s.create(ctx, r, nil, snaps...)
}

func (s *Store) create(ctx context.Context, r recipe.Recipe, from *Origin, snaps ...SnapshotInput) (int64, error) {
	who, err := identity(ctx)
	if err != nil {
		return 0, err
	}
	hid, err := scope(ctx)
	if err != nil {
		return 0, err
	}
	if err := r.Validate(); err != nil {
		return 0, fmt.Errorf("validate recipe: %w", err)
	}
	var id int64
	err = s.withTx(ctx, func(q *storedb.Queries) (err error) {
		if id, err = s.insertRecipe(ctx, q, hid, r, from, who); err != nil {
			return err
		}
		seen := map[SnapshotKind]bool{}
		for _, sn := range snaps {
			if seen[sn.Kind] {
				return fmt.Errorf("%w: kind %s given twice", ErrSnapshotExists, sn.Kind)
			}
			seen[sn.Kind] = true
			if err := s.insertVersionedSnapshot(ctx, q, hid, id, sn, who); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// insertRecipe writes a validated document as a new recipe of household hid
// inside the transaction q is bound to.
func (s *Store) insertRecipe(ctx context.Context, q *storedb.Queries, hid int64, r recipe.Recipe, from *Origin, who string) (int64, error) {
	c, err := documentColumns(r)
	if err != nil {
		return 0, err
	}
	var originID, originVersion sql.NullInt64
	if from != nil {
		originID = sql.NullInt64{Int64: from.RecipeID, Valid: true}
		originVersion = sql.NullInt64{Int64: from.Version, Valid: true}
	}
	now := s.stamp()
	id, err := q.InsertRecipe(ctx, storedb.InsertRecipeParams{
		HouseholdID: hid, Name: c.Name, Description: c.Description, SourceUrl: c.SourceUrl, Author: c.Author,
		YieldQty: c.YieldQty, YieldUnit: c.YieldUnit, Servings: c.Servings,
		CaloriesPerServing: c.CaloriesPerServing, Rating: c.Rating,
		PrepNs: c.PrepNs, CookNs: c.CookNs, TotalNs: c.TotalNs,
		ClonedFromRecipeID: originID, ClonedFromVersion: originVersion,
		CreatedAt: now, CreatedBy: who, UpdatedAt: now, UpdatedBy: who,
	})
	if err != nil {
		return 0, wrapWrite("write recipe", err)
	}
	// The recipe row goes in before any read: a transaction whose first
	// statement is a read cannot upgrade to a write if another writer has
	// committed since, and fails with SQLITE_BUSY at once.
	units, err := loadUnits(ctx, q)
	if err != nil {
		return 0, err
	}
	if err := insertChildren(ctx, q, hid, id, r, units); err != nil {
		return 0, err
	}
	if err := setVideo(ctx, q, hid, id, r.VideoID); err != nil {
		return 0, err
	}
	if err := setRecipeFields(ctx, q, hid, id, r); err != nil {
		return 0, err
	}
	return id, nil
}

// Get returns the current document of recipe id, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id int64) (recipe.Recipe, error) {
	hid, err := scope(ctx)
	if err != nil {
		return recipe.Recipe{}, err
	}
	var r recipe.Recipe
	err = s.read(ctx, func(q *storedb.Queries) error {
		units, err := loadUnits(ctx, q)
		if err != nil {
			return err
		}
		r, err = loadDocument(ctx, q, hid, id, units)
		return err
	})
	if err != nil {
		return recipe.Recipe{}, err
	}
	return r, nil
}

// Info returns the bookkeeping of recipe id, or ErrNotFound.
func (s *Store) Info(ctx context.Context, id int64) (Info, error) {
	hid, err := scope(ctx)
	if err != nil {
		return Info{}, err
	}
	return loadInfo(ctx, s.q, hid, id)
}

func loadInfo(ctx context.Context, q *storedb.Queries, hid, id int64) (Info, error) {
	row, err := q.GetRecipeInfo(ctx, storedb.GetRecipeInfoParams{ID: id, HouseholdID: hid})
	if errors.Is(err, sql.ErrNoRows) {
		return Info{}, fmt.Errorf("%w: recipe %d", ErrNotFound, id)
	}
	if err != nil {
		return Info{}, fmt.Errorf("read recipe %d info: %w", id, err)
	}
	in := Info{ID: id, Version: row.Version, CreatedBy: row.CreatedBy, UpdatedBy: row.UpdatedBy}
	if in.CreatedAt, err = parseTime(row.CreatedAt); err != nil {
		return Info{}, err
	}
	if in.UpdatedAt, err = parseTime(row.UpdatedAt); err != nil {
		return Info{}, err
	}
	if row.ClonedFromRecipeID.Valid {
		in.ClonedFrom = &Origin{RecipeID: row.ClonedFromRecipeID.Int64, Version: row.ClonedFromVersion.Int64}
	}
	return in, nil
}

// Update replaces the document of recipe id with r. In one transaction it
// writes a revision holding the document as it was, with the time and author
// of this edit, and bumps the version. An invalid r, or a missing recipe,
// writes nothing.
func (s *Store) Update(ctx context.Context, id int64, r recipe.Recipe) error {
	return s.update(ctx, id, r, nil)
}

// ErrStaleVersion is returned by UpdateIfVersion when the recipe has been
// changed since the caller loaded it.
var ErrStaleVersion = errors.New("the recipe was changed since it was loaded")

// UpdateIfVersion is Update applied only if recipe id is still at
// expectedVersion, checked in the same write transaction as the write. A stale
// version is ErrStaleVersion and writes nothing, not even a revision.
func (s *Store) UpdateIfVersion(ctx context.Context, id, expectedVersion int64, r recipe.Recipe) error {
	return s.update(ctx, id, r, &expectedVersion)
}

func (s *Store) update(ctx context.Context, id int64, r recipe.Recipe, expected *int64) error {
	who, err := identity(ctx)
	if err != nil {
		return err
	}
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("validate recipe: %w", err)
	}
	return s.withTx(ctx, func(q *storedb.Queries) error {
		if err := lockRecipe(ctx, q, hid, id); err != nil {
			return err
		}
		if expected != nil {
			cur, err := loadInfo(ctx, q, hid, id)
			if err != nil {
				return err
			}
			if cur.Version != *expected {
				return fmt.Errorf("%w: recipe %d is at version %d, not %d", ErrStaleVersion, id, cur.Version, *expected)
			}
		}
		return s.replace(ctx, q, hid, id, r, who, changeEdit, nil)
	})
}

// lockRecipe reports ErrNotFound for a missing recipe, or one of another
// household, and otherwise touches the row with a no-op write. Write
// transactions begin IMMEDIATE, so the write lock is already held when this
// runs; the touch is what makes the existence check part of the write.
func lockRecipe(ctx context.Context, q *storedb.Queries, hid, id int64) error {
	n, err := q.LockRecipe(ctx, storedb.LockRecipeParams{ID: id, HouseholdID: hid})
	if err != nil {
		return fmt.Errorf("lock recipe %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: recipe %d", ErrNotFound, id)
	}
	return nil
}

// replace writes r over recipe id inside the transaction q is bound to, after
// saving the current document as a revision. The caller has locked the recipe
// and validated r.
func (s *Store) replace(ctx context.Context, q *storedb.Queries, hid, id int64, r recipe.Recipe, who, change string, restored *int64) error {
	units, err := loadUnits(ctx, q)
	if err != nil {
		return err
	}
	cur, err := loadInfo(ctx, q, hid, id)
	if err != nil {
		return err
	}
	old, err := loadDocument(ctx, q, hid, id, units)
	if err != nil {
		return err
	}
	now := s.stamp()
	if err := insertRevision(ctx, q, hid, id, cur.Version, old, units, change, restored, now, who); err != nil {
		return err
	}
	c, err := documentColumns(r)
	if err != nil {
		return err
	}
	c.UpdatedAt, c.UpdatedBy, c.ID, c.HouseholdID = now, who, id, hid
	if err := q.UpdateRecipeDocument(ctx, c); err != nil {
		return wrapWrite("update recipe", err)
	}
	if err := setVideo(ctx, q, hid, id, r.VideoID); err != nil {
		return err
	}
	if err := setRecipeFields(ctx, q, hid, id, r); err != nil {
		return err
	}
	if err := clearChildren(ctx, q, hid, id); err != nil {
		return err
	}
	return insertChildren(ctx, q, hid, id, r, units)
}

// Delete removes recipe id (of the request's household) with its revisions, notes, made-it entries and
// source snapshots, or returns ErrNotFound. A clone of it survives, losing
// only the pointer to its origin.
func (s *Store) Delete(ctx context.Context, id int64) error {
	if _, err := identity(ctx); err != nil {
		return err
	}
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	// Through withTx, not the bare pool, so the delete queues with the other
	// writers instead of contending with them for the SQLite write lock (#104).
	return s.withTx(ctx, func(q *storedb.Queries) error {
		n, err := q.DeleteRecipe(ctx, storedb.DeleteRecipeParams{ID: id, HouseholdID: hid})
		if err != nil {
			return fmt.Errorf("delete recipe %d: %w", id, err)
		}
		if n == 0 {
			return fmt.Errorf("%w: recipe %d", ErrNotFound, id)
		}
		return nil
	})
}

// Clone copies recipe id into a new, independent recipe at version 1 that
// records the recipe and version it was copied from. Notes, made-it entries,
// revisions and source snapshots stay with the original.
func (s *Store) Clone(ctx context.Context, id int64) (int64, error) {
	who, err := identity(ctx)
	if err != nil {
		return 0, err
	}
	hid, err := scope(ctx)
	if err != nil {
		return 0, err
	}
	var cloneID int64
	err = s.withTx(ctx, func(q *storedb.Queries) error {
		if err := lockRecipe(ctx, q, hid, id); err != nil {
			return err
		}
		units, err := loadUnits(ctx, q)
		if err != nil {
			return err
		}
		cur, err := loadInfo(ctx, q, hid, id)
		if err != nil {
			return err
		}
		doc, err := loadDocument(ctx, q, hid, id, units)
		if err != nil {
			return err
		}
		cloneID, err = s.insertRecipe(ctx, q, hid, doc, &Origin{RecipeID: id, Version: cur.Version}, who)
		return err
	})
	if err != nil {
		return 0, err
	}
	return cloneID, nil
}
