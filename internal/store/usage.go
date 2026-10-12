package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog"
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
