package catalog

import (
	"context"
	"database/sql"
	"errors"
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
// the recipe ingredient lines, the pantry and the shopping items. Register at start-up, before the store is shared between goroutines.
func (s *Store) OnIngredientMerge(h MergeHook) {
	s.mergeHooks = append(s.mergeHooks, h)
}

// MergeIngredients folds the ingredient loserID into winnerID in one
// transaction: the loser's aliases and its name become aliases of the winner,
// the winner takes the loser's density and USDA id where it has none, every
// registered hook re-points its referencing rows, and the loser is deleted.
// Any failure leaves the catalog and all hooked tables unchanged. It returns
// the merged winner.
//
// A merge is a household's act (ADR-00018): the loser must be one of the
// household's own overlay entries, and the winner another of them or a central
// entry. Two central entries are never merged. Folding into a central entry
// keeps the loser's name and aliases as the household's own aliases of it, and
// its aisle as the household's aisle override when the loser's aisle was
// reviewed and differs, and carries no density or USDA id into the central
// layer.
func (s *Store) MergeIngredients(ctx context.Context, winnerID, loserID int64) (Ingredient, error) {
	if winnerID == loserID {
		return Ingredient{}, fmt.Errorf("%w: cannot merge ingredient %d into itself", ErrInvalid, winnerID)
	}
	scope := scopeOf(ctx)
	if scope == 0 {
		return Ingredient{}, fmt.Errorf("%w: a merge needs a household: central entries are never merged", ErrInvalid)
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
		if _, err := getIngredient(ctx, q, scope, winnerID); err != nil {
			return err
		}
		loser, err := q.GetIngredient(ctx, catalogdb.GetIngredientParams{ID: loserID, Scope: readScope(scope)})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: ingredient %d", ErrNotFound, loserID)
		}
		if err != nil {
			return fmt.Errorf("read ingredient %d: %w", loserID, err)
		}
		if !loser.HouseholdID.Valid {
			return errCentral(fmt.Sprintf("%q is central and cannot be merged away", loser.Name))
		}
		if err := s.fold(ctx, q, tx, winnerID, loser); err != nil {
			return err
		}
		merged, err = getIngredient(ctx, q, scope, winnerID)
		return err
	})
	if err != nil {
		return Ingredient{}, err
	}
	return merged, nil
}

// fold merges the overlay entry loser into winnerID inside tx, for the
// household that holds loser. The winner is a central entry or another overlay
// entry of that household; callers check it.
func (s *Store) fold(ctx context.Context, q *catalogdb.Queries, tx *sql.Tx, winnerID int64, loser catalogdb.Ingredient) error {
	house := loser.HouseholdID
	winner, err := q.GetIngredient(ctx, catalogdb.GetIngredientParams{ID: winnerID, Scope: house})
	if err != nil {
		return fmt.Errorf("read ingredient %d: %w", winnerID, err)
	}
	intoCentral := !winner.HouseholdID.Valid
	// Aliases move before the delete cascades over them; hooks run before the
	// delete because referencing rows would otherwise block it.
	if err := q.MoveIngredientAliases(ctx, catalogdb.MoveIngredientAliasesParams{WinnerID: winnerID, LoserID: loser.ID}); err != nil {
		return fmt.Errorf("move aliases of ingredient %d: %w", loser.ID, err)
	}
	if err := addIngredientAlias(ctx, q, house, loser.Name, winnerID); err != nil {
		return fmt.Errorf("keep name %q of merged ingredient as an alias: %w", loser.Name, err)
	}
	if intoCentral {
		// A name or alias the central entry already answers to is redundant.
		if err := q.PruneRedundantAliases(ctx, catalogdb.PruneRedundantAliasesParams{Owner: house, IngredientID: winnerID}); err != nil {
			return fmt.Errorf("prune aliases of ingredient %d: %w", winnerID, err)
		}
		if err := keepAisle(ctx, q, house.Int64, winner, loser); err != nil {
			return err
		}
	}
	for _, h := range s.mergeHooks {
		if err := h(ctx, tx, loser.ID, winnerID); err != nil {
			return fmt.Errorf("re-point references of ingredient %d: %w", loser.ID, err)
		}
	}
	n, err := q.DeleteIngredient(ctx, loser.ID)
	if err != nil {
		return fmt.Errorf("delete ingredient %d: %w", loser.ID, err)
	}
	if err := requireAffected(n, "ingredient", loser.ID); err != nil {
		return err
	}
	if intoCentral {
		return nil
	}
	if err := q.CarryIngredientData(ctx, catalogdb.CarryIngredientDataParams{
		DensityGPerMl: loser.DensityGPerMl, UsdaFdcID: loser.UsdaFdcID, ID: winnerID,
	}); err != nil {
		return wrapWrite(fmt.Sprintf("carry density and USDA id into ingredient %d", winnerID), err)
	}
	return nil
}

// keepAisle gives the household an aisle override on the central winner when
// the loser had a reviewed aisle of its own that differs, so its shopping list
// keeps its order. An unreviewed loser's aisle is the placeholder ingestion
// proposed, never a choice, and is not kept.
func keepAisle(ctx context.Context, q *catalogdb.Queries, household int64, winner, loser catalogdb.Ingredient) error {
	if loser.Reviewed == 0 || loser.Aisle == winner.Aisle {
		return nil
	}
	err := q.SetAisleOverride(ctx, catalogdb.SetAisleOverrideParams{HouseholdID: household, IngredientID: winner.ID, Aisle: loser.Aisle})
	if err != nil {
		return fmt.Errorf("keep aisle %q on ingredient %d: %w", loser.Aisle, winner.ID, err)
	}
	return nil
}
