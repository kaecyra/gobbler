package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

// Ingredient is a canonical ingredient (ADR-00008). Name is the bare name
// without qualifiers and Aliases hold plurals and synonyms; both are stored
// trimmed and lower-case. DensityGPerML is grams per millilitre and nil when
// unknown, never guessed (ADR-00005). USDAID is the FoodData Central id.
// Reviewed is false for an ingredient proposed by ingestion and not yet
// checked on the catalog page.
type Ingredient struct {
	ID            int64
	Name          string
	Aisle         string
	Aliases       []string
	DensityGPerML *float64
	USDAID        *int64
	Reviewed      bool
}

func (i *Ingredient) prepare() error {
	i.Name = normalize(i.Name)
	if err := validateName(i.Name); err != nil {
		return err
	}
	i.Aisle = normalize(i.Aisle)
	if i.Aisle == "" {
		return fmt.Errorf("%w: ingredient %q has no aisle", ErrInvalid, i.Name)
	}
	if d := i.DensityGPerML; d != nil && (math.IsNaN(*d) || math.IsInf(*d, 0) || *d <= 0) {
		return fmt.Errorf("%w: ingredient %q density %v must be a positive number", ErrInvalid, i.Name, *d)
	}
	if i.USDAID != nil && *i.USDAID <= 0 {
		return fmt.Errorf("%w: ingredient %q USDA id %d must be positive", ErrInvalid, i.Name, *i.USDAID)
	}
	i.Aliases = normalizeAliases(i.Name, i.Aliases)
	return nil
}

const ingredientColumns = `id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed`

func scanIngredient(sc interface{ Scan(...any) error }) (Ingredient, error) {
	var (
		i       Ingredient
		density sql.NullFloat64
		usda    sql.NullInt64
		rev     int
	)
	if err := sc.Scan(&i.ID, &i.Name, &i.Aisle, &density, &usda, &rev); err != nil {
		return Ingredient{}, err
	}
	if density.Valid {
		i.DensityGPerML = &density.Float64
	}
	if usda.Valid {
		i.USDAID = &usda.Int64
	}
	i.Reviewed = rev != 0
	return i, nil
}

// CreateIngredient stores a new ingredient and returns it with its id. Set
// Reviewed to false for an ingredient proposed by ingestion. It returns
// ErrConflict if the name or an alias already belongs to another ingredient.
func (s *Store) CreateIngredient(ctx context.Context, in Ingredient) (Ingredient, error) {
	if err := in.prepare(); err != nil {
		return Ingredient{}, err
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if err := kindIngredient.checkFree(ctx, tx, 0, in.Name, in.Aliases); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO ingredients (name, aisle, density_g_per_ml, usda_fdc_id, reviewed) VALUES (?, ?, ?, ?, ?)`,
			in.Name, in.Aisle, in.DensityGPerML, in.USDAID, boolInt(in.Reviewed))
		if err != nil {
			return wrapWrite("create ingredient "+in.Name, err)
		}
		if in.ID, err = res.LastInsertId(); err != nil {
			return fmt.Errorf("read new ingredient id: %w", err)
		}
		return kindIngredient.replaceAliases(ctx, tx, in.ID, in.Aliases)
	})
	if err != nil {
		return Ingredient{}, err
	}
	return in, nil
}

// UpdateIngredient replaces every field of the ingredient with in.ID,
// including its aliases. It returns ErrNotFound if the id does not exist.
func (s *Store) UpdateIngredient(ctx context.Context, in Ingredient) error {
	if err := in.prepare(); err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := kindIngredient.checkFree(ctx, tx, in.ID, in.Name, in.Aliases); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE ingredients SET name = ?, aisle = ?, density_g_per_ml = ?, usda_fdc_id = ?, reviewed = ? WHERE id = ?`,
			in.Name, in.Aisle, in.DensityGPerML, in.USDAID, boolInt(in.Reviewed), in.ID)
		if err != nil {
			return wrapWrite(fmt.Sprintf("update ingredient %d", in.ID), err)
		}
		if err := requireAffected(res, "ingredient", in.ID); err != nil {
			return err
		}
		return kindIngredient.replaceAliases(ctx, tx, in.ID, in.Aliases)
	})
}

// GetIngredient returns the ingredient with the given id, or ErrNotFound.
func (s *Store) GetIngredient(ctx context.Context, id int64) (Ingredient, error) {
	return getIngredient(ctx, s.db, id)
}

func getIngredient(ctx context.Context, q queryer, id int64) (Ingredient, error) {
	i, err := scanIngredient(q.QueryRowContext(ctx, `SELECT `+ingredientColumns+` FROM ingredients WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Ingredient{}, fmt.Errorf("%w: ingredient %d", ErrNotFound, id)
	}
	if err != nil {
		return Ingredient{}, fmt.Errorf("read ingredient %d: %w", id, err)
	}
	if i.Aliases, err = kindIngredient.aliases(ctx, q, id); err != nil {
		return Ingredient{}, err
	}
	return i, nil
}

// ListIngredients returns every ingredient ordered by name.
func (s *Store) ListIngredients(ctx context.Context) ([]Ingredient, error) {
	return s.listIngredients(ctx, ``)
}

// ListUnreviewedIngredients returns the ingredients awaiting review, ordered
// by name, for the catalog cleanup page.
func (s *Store) ListUnreviewedIngredients(ctx context.Context) ([]Ingredient, error) {
	return s.listIngredients(ctx, `WHERE reviewed = 0`)
}

func (s *Store) listIngredients(ctx context.Context, where string) ([]Ingredient, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+ingredientColumns+` FROM ingredients `+where+` ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list ingredients: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Ingredient
	for rows.Next() {
		i, err := scanIngredient(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ingredient: %w", err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list ingredients: %w", err)
	}
	_ = rows.Close() // free the connection before the alias read
	aliases, err := kindIngredient.allAliases(ctx, s.db)
	if err != nil {
		return nil, err
	}
	for n := range out {
		out[n].Aliases = aliases[out[n].ID]
	}
	return out, nil
}

// MatchIngredient finds the ingredient whose name or alias equals term,
// ignoring case and tolerating plural and singular forms. It returns
// (Ingredient{}, false, nil) when nothing matches; that is an answer, not an
// error, and nothing is created. This is the method internal/parse consumes
// through an interface it declares itself.
func (s *Store) MatchIngredient(ctx context.Context, term string) (Ingredient, bool, error) {
	id, ok, err := kindIngredient.match(ctx, s.db, term)
	if err != nil || !ok {
		return Ingredient{}, false, err
	}
	i, err := getIngredient(ctx, s.db, id)
	if err != nil {
		return Ingredient{}, false, err
	}
	return i, true, nil
}
