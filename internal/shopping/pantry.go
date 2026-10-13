package shopping

import (
	"context"
	"fmt"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/shopping/shoppingdb"
)

// The pantry is the set of ingredients always on hand (ADR-00009): not
// inventory, so no quantities. It applies to every list of the household.

// AddToPantry adds an ingredient to the pantry. Adding one already there is not
// an error and changes nothing. It returns ErrUnknownReference for an
// ingredient not in the catalog.
func (s *Store) AddToPantry(ctx context.Context, ingredient recipe.IngredientID) error {
	return s.changePantry(ctx, func(q *shoppingdb.Queries, hid int64) (int64, error) {
		n, err := q.AddPantryIngredient(ctx, shoppingdb.AddPantryIngredientParams{HouseholdID: hid, IngredientID: int64(ingredient)})
		return n, wrapWrite(fmt.Sprintf("add ingredient %d to the pantry", ingredient), err)
	})
}

// RemoveFromPantry takes an ingredient out of the pantry. Removing one that is
// not there is not an error.
func (s *Store) RemoveFromPantry(ctx context.Context, ingredient recipe.IngredientID) error {
	return s.changePantry(ctx, func(q *shoppingdb.Queries, hid int64) (int64, error) {
		n, err := q.RemovePantryIngredient(ctx, shoppingdb.RemovePantryIngredientParams{HouseholdID: hid, IngredientID: int64(ingredient)})
		if err != nil {
			return 0, fmt.Errorf("remove ingredient %d from the pantry: %w", ingredient, err)
		}
		return n, nil
	})
}

// Pantry returns the pantry's ingredients, ordered by id.
func (s *Store) Pantry(ctx context.Context) ([]recipe.IngredientID, error) {
	hid, err := scope(ctx)
	if err != nil {
		return nil, err
	}
	ids, err := s.q.ListPantry(ctx, hid)
	if err != nil {
		return nil, fmt.Errorf("read pantry: %w", err)
	}
	out := make([]recipe.IngredientID, len(ids))
	for i, id := range ids {
		out[i] = recipe.IngredientID(id)
	}
	return out, nil
}

// changePantry runs a pantry write and, when it changed a row, notifies every
// list of the household, because the pantry decides what each of them shows.
func (s *Store) changePantry(ctx context.Context, write func(q *shoppingdb.Queries, hid int64) (int64, error)) error {
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	var lists []int64
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		n, err := write(q, hid)
		if err != nil || n == 0 {
			return err
		}
		lists, err = q.ListListIDs(ctx, hid)
		if err != nil {
			return fmt.Errorf("read lists: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.notify(lists...)
	return nil
}
