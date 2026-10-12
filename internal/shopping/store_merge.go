package shopping

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/shopping/shoppingdb"
)

// mergeRegistrar is what the shopping store needs of the catalog: somewhere to
// register its ingredient merge hooks.
type mergeRegistrar interface {
	OnIngredientMerge(h catalog.MergeHook)
}

// RegisterMergeHooks registers, with the catalog, the hooks that re-point every
// table of this package that references ingredients(id): manual items and the
// pantry, plus the check rows keyed by ingredient so a check follows its
// ingredient. Call it at start-up, before the catalog is shared between
// goroutines. Without it a merge of an ingredient that a list or the pantry
// uses fails on the foreign key.
func (s *Store) RegisterMergeHooks(c mergeRegistrar) {
	c.OnIngredientMerge(repointShopping)
}

func repointShopping(ctx context.Context, tx *sql.Tx, loserID, winnerID int64) error {
	q := shoppingdb.New(tx)
	if err := q.RepointManualItems(ctx, shoppingdb.RepointManualItemsParams{
		WinnerID: sql.NullInt64{Int64: winnerID, Valid: true},
		LoserID:  sql.NullInt64{Int64: loserID, Valid: true},
	}); err != nil {
		return fmt.Errorf("re-point manual items: %w", err)
	}
	if err := q.RepointPantry(ctx, shoppingdb.RepointPantryParams{WinnerID: winnerID, LoserID: loserID}); err != nil {
		return fmt.Errorf("re-point pantry: %w", err)
	}
	if err := q.DeletePantryIngredient(ctx, loserID); err != nil {
		return fmt.Errorf("drop duplicate pantry row: %w", err)
	}
	if err := q.RepointChecks(ctx, shoppingdb.RepointChecksParams{
		WinnerID: strconv.FormatInt(winnerID, 10), LoserID: strconv.FormatInt(loserID, 10),
	}); err != nil {
		return fmt.Errorf("re-point checks: %w", err)
	}
	if err := q.DeleteChecksForIngredient(ctx, strconv.FormatInt(loserID, 10)); err != nil {
		return fmt.Errorf("drop duplicate checks: %w", err)
	}
	return nil
}
