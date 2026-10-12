package shopping

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/kaecyra/gobbler/internal/catalog"
)

// usageRegistrar is what the shopping store needs of the catalog's usage
// counting: somewhere to register its counter.
type usageRegistrar interface {
	OnIngredientUsage(c catalog.UsageCounter)
}

// RegisterUsageCounters registers, with the catalog, the counter for the rows
// RegisterMergeHooks re-points: manual items, pantry rows and the check rows
// keyed by the ingredient. Call it at start-up, next to RegisterMergeHooks.
func (s *Store) RegisterUsageCounters(c usageRegistrar) {
	c.OnIngredientUsage(s.countUsage)
}

func (s *Store) countUsage(ctx context.Context, ingredientID int64) (int64, error) {
	manual, err := s.q.CountIngredientManualItems(ctx, sql.NullInt64{Int64: ingredientID, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("count manual items of ingredient %d: %w", ingredientID, err)
	}
	pantry, err := s.q.CountIngredientPantry(ctx, ingredientID)
	if err != nil {
		return 0, fmt.Errorf("count pantry rows of ingredient %d: %w", ingredientID, err)
	}
	checks, err := s.q.CountIngredientChecks(ctx, strconv.FormatInt(ingredientID, 10))
	if err != nil {
		return 0, fmt.Errorf("count checks of ingredient %d: %w", ingredientID, err)
	}
	return manual + pantry + checks, nil
}
