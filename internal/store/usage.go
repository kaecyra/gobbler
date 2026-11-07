package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// usageRegistrar is what the store needs of the catalog's usage counting:
// somewhere to register its counter.
type usageRegistrar interface {
	OnIngredientUsage(c catalog.UsageCounter)
}

// RegisterUsageCounters registers, with the catalog, the counters for the
// tables whose rows a merge re-points: the recipe lines. Revision documents
// are rewritten by the merge too, but they are history, not recipe lines, so
// they are not counted. Call it at start-up, next to RegisterMergeHooks.
func (s *Store) RegisterUsageCounters(c usageRegistrar) {
	c.OnIngredientUsage(s.countLines)
}

func (s *Store) countLines(ctx context.Context, ingredientID int64) (int64, error) {
	n, err := s.q.CountIngredientLines(ctx, sql.NullInt64{Int64: ingredientID, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("count recipe lines of ingredient %d: %w", ingredientID, err)
	}
	return n, nil
}

// RecipeCountsByIngredient returns, for each ingredient the household's recipes
// use, how many of its recipes use it, keyed by ingredient id. An ingredient no
// recipe uses is absent. Unlike the merge counters it counts recipes, not rows,
// and only the household's own: the catalog page shows it beside central
// entries too, and another household's use of those is not this household's to
// see (ADR-00018).
func (s *Store) RecipeCountsByIngredient(ctx context.Context) (map[int64]int64, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64]int64{}
	err = s.read(ctx, func(q *storedb.Queries) error {
		rows, err := q.CountRecipesByIngredient(ctx, hid)
		if err != nil {
			return readErr("recipes by ingredient", err)
		}
		for _, r := range rows {
			out[r.IngredientID.Int64] = r.Recipes
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
