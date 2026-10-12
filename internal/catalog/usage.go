package catalog

import (
	"context"
	"fmt"
)

// UsageCounter reports how many rows of one referencing table name the
// ingredient. It is the read-only counterpart of MergeHook: the table's owner
// registers one, so the catalog never queries a table it does not own
// (ADR-00014).
type UsageCounter func(ctx context.Context, ingredientID int64) (int64, error)

// Usage counts the references to catalog ingredients that the registered
// counters can see. It wraps a Store only to check that the ingredient exists;
// it holds no state of the Store's own.
type Usage struct {
	s        *Store
	counters []UsageCounter
}

// NewUsage returns a Usage over s with no counter registered.
func NewUsage(s *Store) *Usage { return &Usage{s: s} }

// OnIngredientUsage registers a counter that IngredientUsage adds up. Register
// at start-up, before the Usage is shared between goroutines, and register the
// same tables that register a MergeHook so the count matches what a merge
// re-points.
func (u *Usage) OnIngredientUsage(c UsageCounter) {
	u.counters = append(u.counters, c)
}

// IngredientUsage returns how many referencing rows a merge of the ingredient
// would re-point, summed over every registered counter. It returns ErrNotFound
// if the ingredient does not exist.
func (u *Usage) IngredientUsage(ctx context.Context, id int64) (int64, error) {
	if _, err := u.s.GetIngredient(ctx, id); err != nil {
		return 0, err
	}
	var total int64
	for _, c := range u.counters {
		n, err := c(ctx, id)
		if err != nil {
			return 0, fmt.Errorf("count references of ingredient %d: %w", id, err)
		}
		total += n
	}
	return total, nil
}
