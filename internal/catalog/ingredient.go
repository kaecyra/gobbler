package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/kaecyra/gobbler/internal/catalog/catalogdb"
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

func ingredientFrom(r catalogdb.Ingredient) Ingredient {
	i := Ingredient{ID: r.ID, Name: r.Name, Aisle: r.Aisle, Reviewed: r.Reviewed != 0}
	if r.DensityGPerMl.Valid {
		i.DensityGPerML = &r.DensityGPerMl.Float64
	}
	if r.UsdaFdcID.Valid {
		i.USDAID = &r.UsdaFdcID.Int64
	}
	return i
}

// CreateIngredient stores a new ingredient and returns it with its id. Set
// Reviewed to false for an ingredient proposed by ingestion. It returns
// ErrConflict if the name or an alias already belongs to another ingredient.
func (s *Store) CreateIngredient(ctx context.Context, in Ingredient) (Ingredient, error) {
	if err := in.prepare(); err != nil {
		return Ingredient{}, err
	}
	err := s.withTx(ctx, func(q *catalogdb.Queries, _ *sql.Tx) (err error) {
		if err := kindIngredient.checkFree(ctx, q, 0, in.Name, in.Aliases); err != nil {
			return err
		}
		in.ID, err = q.CreateIngredient(ctx, catalogdb.CreateIngredientParams{
			Name: in.Name, Aisle: in.Aisle, DensityGPerMl: nullFloat(in.DensityGPerML),
			UsdaFdcID: nullInt(in.USDAID), Reviewed: boolInt(in.Reviewed),
		})
		if err != nil {
			return wrapWrite("create ingredient "+in.Name, err)
		}
		return kindIngredient.replaceAliases(ctx, q, in.ID, in.Aliases)
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
	return s.withTx(ctx, func(q *catalogdb.Queries, _ *sql.Tx) error {
		if err := kindIngredient.checkFree(ctx, q, in.ID, in.Name, in.Aliases); err != nil {
			return err
		}
		n, err := q.UpdateIngredient(ctx, catalogdb.UpdateIngredientParams{
			Name: in.Name, Aisle: in.Aisle, DensityGPerMl: nullFloat(in.DensityGPerML),
			UsdaFdcID: nullInt(in.USDAID), Reviewed: boolInt(in.Reviewed), ID: in.ID,
		})
		if err != nil {
			return wrapWrite(fmt.Sprintf("update ingredient %d", in.ID), err)
		}
		if err := requireAffected(n, "ingredient", in.ID); err != nil {
			return err
		}
		return kindIngredient.replaceAliases(ctx, q, in.ID, in.Aliases)
	})
}

// GetIngredient returns the ingredient with the given id, or ErrNotFound.
func (s *Store) GetIngredient(ctx context.Context, id int64) (Ingredient, error) {
	return getIngredient(ctx, s.q, id)
}

func getIngredient(ctx context.Context, q *catalogdb.Queries, id int64) (Ingredient, error) {
	row, err := q.GetIngredient(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Ingredient{}, fmt.Errorf("%w: ingredient %d", ErrNotFound, id)
	}
	if err != nil {
		return Ingredient{}, fmt.Errorf("read ingredient %d: %w", id, err)
	}
	i := ingredientFrom(row)
	if i.Aliases, err = kindIngredient.aliasesOf(ctx, q, id); err != nil {
		return Ingredient{}, err
	}
	return i, nil
}

// ListIngredients returns every ingredient ordered by name.
func (s *Store) ListIngredients(ctx context.Context) ([]Ingredient, error) {
	rows, err := s.q.ListIngredients(ctx)
	return s.withIngredientAliases(ctx, rows, err)
}

// ListUnreviewedIngredients returns the ingredients awaiting review, ordered
// by name, for the catalog cleanup page.
func (s *Store) ListUnreviewedIngredients(ctx context.Context) ([]Ingredient, error) {
	rows, err := s.q.ListUnreviewedIngredients(ctx)
	return s.withIngredientAliases(ctx, rows, err)
}

// withIngredientAliases converts the rows of a list query, which failed with
// err if it is set, and attaches every alias in one further query.
func (s *Store) withIngredientAliases(ctx context.Context, rows []catalogdb.Ingredient, err error) ([]Ingredient, error) {
	if err != nil {
		return nil, fmt.Errorf("list ingredients: %w", err)
	}
	var out []Ingredient
	for _, r := range rows {
		out = append(out, ingredientFrom(r))
	}
	aliases, err := kindIngredient.aliasesByID(ctx, s.q)
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
	id, ok, err := kindIngredient.match(ctx, s.q, term)
	if err != nil || !ok {
		return Ingredient{}, false, err
	}
	i, err := getIngredient(ctx, s.q, id)
	if err != nil {
		return Ingredient{}, false, err
	}
	return i, true, nil
}
