package catalog

import (
	"context"
	"database/sql"
	"fmt"
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
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := getIngredient(ctx, tx, winnerID); err != nil {
			return err
		}
		loser, err := getIngredient(ctx, tx, loserID)
		if err != nil {
			return err
		}
		// Aliases move before the delete cascades over them; hooks run
		// before the delete because referencing rows would otherwise block
		// it; the loser row goes before the winner takes its unique USDA id.
		if _, err := tx.ExecContext(ctx, `UPDATE ingredient_aliases SET ingredient_id = ? WHERE ingredient_id = ?`, winnerID, loserID); err != nil {
			return fmt.Errorf("move aliases of ingredient %d: %w", loserID, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ingredient_aliases (alias, ingredient_id) VALUES (?, ?)`, loser.Name, winnerID); err != nil {
			return wrapWrite(fmt.Sprintf("keep name %q of merged ingredient as an alias", loser.Name), err)
		}
		for _, h := range s.mergeHooks {
			if err := h(ctx, tx, loserID, winnerID); err != nil {
				return fmt.Errorf("re-point references of ingredient %d: %w", loserID, err)
			}
		}
		if err := kindIngredient.delete(ctx, tx, loserID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE ingredients SET density_g_per_ml = COALESCE(density_g_per_ml, ?), usda_fdc_id = COALESCE(usda_fdc_id, ?) WHERE id = ?`,
			loser.DensityGPerML, loser.USDAID, winnerID); err != nil {
			return wrapWrite(fmt.Sprintf("carry density and USDA id into ingredient %d", winnerID), err)
		}
		merged, err = getIngredient(ctx, tx, winnerID)
		return err
	})
	if err != nil {
		return Ingredient{}, err
	}
	return merged, nil
}
