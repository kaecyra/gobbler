package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
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

func (s *Store) create(ctx context.Context, r recipe.Recipe, from *Origin) (int64, error) {
	who, err := identity(ctx)
	if err != nil {
		return 0, err
	}
	if err := r.Validate(); err != nil {
		return 0, fmt.Errorf("validate recipe: %w", err)
	}
	var id int64
	err = s.withTx(ctx, func(tx *sql.Tx) (err error) {
		id, err = s.insertRecipe(ctx, tx, r, from, who)
		return err
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// insertRecipe writes a validated document as a new recipe inside tx.
func (s *Store) insertRecipe(ctx context.Context, tx *sql.Tx, r recipe.Recipe, from *Origin, who string) (int64, error) {
	cols, err := documentColumns(r)
	if err != nil {
		return 0, err
	}
	var originID, originVersion sql.NullInt64
	if from != nil {
		originID = sql.NullInt64{Int64: from.RecipeID, Valid: true}
		originVersion = sql.NullInt64{Int64: from.Version, Valid: true}
	}
	now := s.stamp()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO recipes (name, description, source_url, author, yield_qty, yield_unit, servings, calories_per_serving, rating,
		   prep_ns, cook_ns, total_ns, cloned_from_recipe_id, cloned_from_version, created_at, created_by, updated_at, updated_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		append(cols, originID, originVersion, now, who, now, who)...)
	if err != nil {
		return 0, wrapWrite("write recipe", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("recipe id: %w", err)
	}
	// The recipe row goes in before any read: a transaction whose first
	// statement is a read cannot upgrade to a write if another writer has
	// committed since, and fails with SQLITE_BUSY at once.
	units, err := loadUnits(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := insertChildren(ctx, tx, id, r, units); err != nil {
		return 0, err
	}
	return id, nil
}

// Get returns the current document of recipe id, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id int64) (recipe.Recipe, error) {
	var r recipe.Recipe
	err := s.read(ctx, func(q queryer) error {
		units, err := loadUnits(ctx, q)
		if err != nil {
			return err
		}
		r, err = loadDocument(ctx, q, id, units)
		return err
	})
	if err != nil {
		return recipe.Recipe{}, err
	}
	return r, nil
}

// Info returns the bookkeeping of recipe id, or ErrNotFound.
func (s *Store) Info(ctx context.Context, id int64) (Info, error) {
	return loadInfo(ctx, s.db, id)
}

func loadInfo(ctx context.Context, q queryer, id int64) (Info, error) {
	var (
		in                      Info
		created, updated        string
		originID, originVersion sql.NullInt64
	)
	err := q.QueryRowContext(ctx,
		`SELECT version, created_at, created_by, updated_at, updated_by, cloned_from_recipe_id, cloned_from_version
		 FROM recipes WHERE id = ?`, id).
		Scan(&in.Version, &created, &in.CreatedBy, &updated, &in.UpdatedBy, &originID, &originVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Info{}, fmt.Errorf("%w: recipe %d", ErrNotFound, id)
	}
	if err != nil {
		return Info{}, fmt.Errorf("read recipe %d info: %w", id, err)
	}
	in.ID = id
	if in.CreatedAt, err = parseTime(created); err != nil {
		return Info{}, err
	}
	if in.UpdatedAt, err = parseTime(updated); err != nil {
		return Info{}, err
	}
	if originID.Valid {
		in.ClonedFrom = &Origin{RecipeID: originID.Int64, Version: originVersion.Int64}
	}
	return in, nil
}

// Update replaces the document of recipe id with r. In one transaction it
// writes a revision holding the document as it was, with the time and author
// of this edit, and bumps the version. An invalid r, or a missing recipe,
// writes nothing.
func (s *Store) Update(ctx context.Context, id int64, r recipe.Recipe) error {
	who, err := identity(ctx)
	if err != nil {
		return err
	}
	if err := r.Validate(); err != nil {
		return fmt.Errorf("validate recipe: %w", err)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := lockRecipe(ctx, tx, id); err != nil {
			return err
		}
		return s.replace(ctx, tx, id, r, who, changeEdit, nil)
	})
}

// lockRecipe takes the write lock with a no-op write, so the reads that
// follow see the state the rest of the transaction will change, and reports
// ErrNotFound for a missing recipe.
func lockRecipe(ctx context.Context, tx *sql.Tx, id int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE recipes SET version = version WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("lock recipe %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("lock recipe %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: recipe %d", ErrNotFound, id)
	}
	return nil
}

// replace writes r over recipe id inside tx, after saving the current
// document as a revision. The caller has locked the recipe and validated r.
func (s *Store) replace(ctx context.Context, tx *sql.Tx, id int64, r recipe.Recipe, who, change string, restored *int64) error {
	units, err := loadUnits(ctx, tx)
	if err != nil {
		return err
	}
	cur, err := loadInfo(ctx, tx, id)
	if err != nil {
		return err
	}
	old, err := loadDocument(ctx, tx, id, units)
	if err != nil {
		return err
	}
	now := s.stamp()
	if err := insertRevision(ctx, tx, id, cur.Version, old, units, change, restored, now, who); err != nil {
		return err
	}
	cols, err := documentColumns(r)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE recipes SET `+documentSetSQL+`, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ?`,
		append(cols, now, who, id)...); err != nil {
		return wrapWrite("update recipe", err)
	}
	if err := clearChildren(ctx, tx, id); err != nil {
		return err
	}
	return insertChildren(ctx, tx, id, r, units)
}

// Delete removes recipe id with its revisions, notes, made-it entries and
// source snapshots, or returns ErrNotFound. A clone of it survives, losing
// only the pointer to its origin.
func (s *Store) Delete(ctx context.Context, id int64) error {
	if _, err := identity(ctx); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM recipes WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete recipe %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete recipe %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: recipe %d", ErrNotFound, id)
	}
	return nil
}

// Clone copies recipe id into a new, independent recipe at version 1 that
// records the recipe and version it was copied from. Notes, made-it entries,
// revisions and source snapshots stay with the original.
func (s *Store) Clone(ctx context.Context, id int64) (int64, error) {
	who, err := identity(ctx)
	if err != nil {
		return 0, err
	}
	var cloneID int64
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if err := lockRecipe(ctx, tx, id); err != nil {
			return err
		}
		units, err := loadUnits(ctx, tx)
		if err != nil {
			return err
		}
		cur, err := loadInfo(ctx, tx, id)
		if err != nil {
			return err
		}
		doc, err := loadDocument(ctx, tx, id, units)
		if err != nil {
			return err
		}
		cloneID, err = s.insertRecipe(ctx, tx, doc, &Origin{RecipeID: id, Version: cur.Version}, who)
		return err
	})
	if err != nil {
		return 0, err
	}
	return cloneID, nil
}
