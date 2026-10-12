package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// Kinds of change recorded on a revision: what replaced the stored document.
const (
	changeEdit    = "edit"
	changeRestore = "restore"
)

// Revision describes one saved earlier version of a recipe. Version is the
// version of the document it holds. CreatedAt and Author are the time and
// author of the edit that replaced it. A revision written by a restore has
// Restored set to the version that was restored.
type Revision struct {
	Version   int64
	CreatedAt time.Time
	Author    string
	Restored  *int64
}

func insertRevision(ctx context.Context, tx *sql.Tx, recipeID, version int64, doc recipe.Recipe, units *unitTable, change string, restored *int64, now, who string) error {
	body, err := encodeDocument(doc, units)
	if err != nil {
		return fmt.Errorf("revision of recipe %d: %w", recipeID, err)
	}
	var restoredVersion sql.NullInt64
	if restored != nil {
		restoredVersion = sql.NullInt64{Int64: *restored, Valid: true}
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO revisions (recipe_id, version, document, change_kind, restored_version, created_at, created_by)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		recipeID, version, string(body), change, restoredVersion, now, who)
	if err != nil {
		return wrapWrite(fmt.Sprintf("write revision %d of recipe %d", version, recipeID), err)
	}
	revID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("revision id: %w", err)
	}
	d, err := decodeDocument(body)
	if err != nil {
		return err
	}
	for _, ing := range d.ingredientIDs() {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO revision_ingredients (revision_id, ingredient_id) VALUES (?, ?)`, revID, ing); err != nil {
			return wrapWrite(fmt.Sprintf("index ingredient %d of revision %d", ing, version), err)
		}
	}
	return nil
}

// ListRevisions returns the saved earlier versions of recipe id, oldest first.
// The current version is not among them; it is the recipe itself. A recipe
// never edited has none; a missing recipe is ErrNotFound.
func (s *Store) ListRevisions(ctx context.Context, id int64) ([]Revision, error) {
	var out []Revision
	err := s.read(ctx, func(q queryer) error {
		if _, err := loadInfo(ctx, q, id); err != nil {
			return err
		}
		return collect(ctx, q, "revisions",
			`SELECT version, created_at, created_by, restored_version FROM revisions WHERE recipe_id = ? ORDER BY version`,
			[]any{id}, func(rows *sql.Rows) error {
				var (
					rev      Revision
					at       string
					restored sql.NullInt64
				)
				if err := rows.Scan(&rev.Version, &at, &rev.Author, &restored); err != nil {
					return err
				}
				var err error
				if rev.CreatedAt, err = parseTime(at); err != nil {
					return err
				}
				if restored.Valid {
					rev.Restored = &restored.Int64
				}
				out = append(out, rev)
				return nil
			})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetVersion returns the document of recipe id as it was at version: from the
// revision of that version, or the live document when version is current.
// An unknown recipe or version is ErrNotFound.
func (s *Store) GetVersion(ctx context.Context, id, version int64) (recipe.Recipe, error) {
	var r recipe.Recipe
	err := s.read(ctx, func(q queryer) error {
		units, err := loadUnits(ctx, q)
		if err != nil {
			return err
		}
		r, err = loadVersion(ctx, q, id, version, units)
		return err
	})
	if err != nil {
		return recipe.Recipe{}, err
	}
	return r, nil
}

func loadVersion(ctx context.Context, q queryer, id, version int64, units *unitTable) (recipe.Recipe, error) {
	cur, err := loadInfo(ctx, q, id)
	if err != nil {
		return recipe.Recipe{}, err
	}
	if version == cur.Version {
		return loadDocument(ctx, q, id, units)
	}
	var body string
	err = q.QueryRowContext(ctx, `SELECT document FROM revisions WHERE recipe_id = ? AND version = ?`, id, version).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return recipe.Recipe{}, fmt.Errorf("%w: version %d of recipe %d", ErrNotFound, version, id)
	}
	if err != nil {
		return recipe.Recipe{}, fmt.Errorf("read version %d of recipe %d: %w", version, id, err)
	}
	d, err := decodeDocument([]byte(body))
	if err != nil {
		return recipe.Recipe{}, fmt.Errorf("version %d of recipe %d: %w", version, id, err)
	}
	r, err := d.toRecipe(units)
	if err != nil {
		return recipe.Recipe{}, fmt.Errorf("version %d of recipe %d: %w", version, id, err)
	}
	return r, nil
}

// Restore makes the document of an earlier version the current one. It is an
// edit like any other: the document it replaces is saved as a revision (marked
// as written by a restore of version) and the version number grows, so no
// revision is removed or overwritten by it. (Separately, an ingredient merge
// re-points catalog ids inside stored revision documents; see
// RegisterMergeHooks.)
func (s *Store) Restore(ctx context.Context, id, version int64) error {
	who, err := identity(ctx)
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := lockRecipe(ctx, tx, id); err != nil {
			return err
		}
		units, err := loadUnits(ctx, tx)
		if err != nil {
			return err
		}
		target, err := loadVersion(ctx, tx, id, version, units)
		if err != nil {
			return err
		}
		if err := target.Validate(); err != nil {
			return fmt.Errorf("validate version %d of recipe %d: %w", version, id, err)
		}
		return s.replace(ctx, tx, id, target, who, changeRestore, &version)
	})
}
