package catalog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/catalog/catalogdb"
)

// MergeHook re-points the rows of one referencing table from the ingredient
// being removed to the one that survives. It runs inside the merge
// transaction and must use tx for every statement, so a failure anywhere
// rolls the whole merge back (ADR-00008 obligation 4).
type MergeHook func(ctx context.Context, tx *sql.Tx, loserID, winnerID int64) error

// OnIngredientMerge registers a hook that MergeIngredients calls for every
// merge. Every table with a foreign key to ingredients(id) must register one:
// the recipe ingredient lines (w1-store), and later pantry and shopping
// items. Register at start-up, before the store is shared between goroutines.
func (s *Store) OnIngredientMerge(h MergeHook) {
	s.mergeHooks = append(s.mergeHooks, h)
}

// MergeIngredients folds the ingredient loserID into winnerID in one
// transaction: the loser's aliases and its name become aliases of the winner,
// the winner takes the loser's density and USDA id where it has none, every
// registered hook re-points its referencing rows, and the loser is deleted.
// Any failure leaves the catalog and all hooked tables unchanged. It returns
// the merged winner.
func (s *Store) MergeIngredients(ctx context.Context, winnerID, loserID int64) (Ingredient, error) {
	if winnerID == loserID {
		return Ingredient{}, fmt.Errorf("%w: cannot merge ingredient %d into itself", ErrInvalid, winnerID)
	}
	var merged Ingredient
	err := s.withTx(ctx, func(q *catalogdb.Queries, tx *sql.Tx) error {
		// The winner row is written before anything is read, so the
		// transaction takes the write lock up front (db.WithTx). A read first
		// would pin a snapshot, and a concurrent writer committing before our
		// first write would fail the merge with SQLITE_BUSY (#59). The lock
		// doubles as the winner's existence check.
		n, err := q.LockIngredient(ctx, winnerID)
		if err != nil {
			return fmt.Errorf("lock ingredient %d: %w", winnerID, err)
		}
		if err := requireAffected(n, "ingredient", winnerID); err != nil {
			return err
		}
		loser, err := getIngredient(ctx, q, loserID)
		if err != nil {
			return err
		}
		// Aliases move before the delete cascades over them; hooks run
		// before the delete because referencing rows would otherwise block
		// it; the loser row goes before the winner takes its unique USDA id.
		if err := q.MoveIngredientAliases(ctx, catalogdb.MoveIngredientAliasesParams{WinnerID: winnerID, LoserID: loserID}); err != nil {
			return fmt.Errorf("move aliases of ingredient %d: %w", loserID, err)
		}
		if err := q.AddIngredientAlias(ctx, catalogdb.AddIngredientAliasParams{Alias: loser.Name, IngredientID: winnerID}); err != nil {
			return wrapWrite(fmt.Sprintf("keep name %q of merged ingredient as an alias", loser.Name), err)
		}
		for _, h := range s.mergeHooks {
			if err := h(ctx, tx, loserID, winnerID); err != nil {
				return fmt.Errorf("re-point references of ingredient %d: %w", loserID, err)
			}
		}
		if err := kindIngredient.delete(ctx, q, loserID); err != nil {
			return err
		}
		if err := q.CarryIngredientData(ctx, catalogdb.CarryIngredientDataParams{
			DensityGPerMl: nullFloat(loser.DensityGPerML), UsdaFdcID: nullInt(loser.USDAID), ID: winnerID,
		}); err != nil {
			return wrapWrite(fmt.Sprintf("carry density and USDA id into ingredient %d", winnerID), err)
		}
		merged, err = getIngredient(ctx, q, winnerID)
		return err
	})
	if err != nil {
		return Ingredient{}, err
	}
	return merged, nil
}
