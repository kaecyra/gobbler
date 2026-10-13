package shopping

import (
	"context"
	"fmt"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/shopping/shoppingdb"
)

// AddEntry puts recipe recipeID on list listID at the given multiplier and
// returns the entry id. It returns ErrNotFound for a missing list,
// ErrUnknownReference for a missing recipe, ErrDuplicate when the recipe is
// already on the list (change its multiplier instead) and ErrInvalid for a
// multiplier that is not positive.
func (s *Store) AddEntry(ctx context.Context, listID, recipeID int64, multiplier quantity.Rat) (int64, error) {
	if err := checkMultiplier(multiplier); err != nil {
		return 0, err
	}
	hid, err := scope(ctx)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		if err := lockList(ctx, q, hid, listID); err != nil {
			return err
		}
		var err error
		id, err = q.InsertEntry(ctx, shoppingdb.InsertEntryParams{
			HouseholdID: hid, ListID: listID, RecipeID: recipeID, Multiplier: quantity.EncodeRat(multiplier),
		})
		return wrapWrite(fmt.Sprintf("add recipe %d to list %d", recipeID, listID), err)
	})
	if err != nil {
		return 0, err
	}
	s.notify(listID)
	return id, nil
}

// SetEntryMultiplier changes the multiplier of entry id, or returns ErrNotFound
// or ErrInvalid.
func (s *Store) SetEntryMultiplier(ctx context.Context, id int64, multiplier quantity.Rat) error {
	if err := checkMultiplier(multiplier); err != nil {
		return err
	}
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	var listID int64
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		var err error
		listID, err = q.SetEntryMultiplier(ctx, shoppingdb.SetEntryMultiplierParams{Multiplier: quantity.EncodeRat(multiplier), ID: id, HouseholdID: hid})
		return rowOrNotFound("entry", id, err)
	})
	if err != nil {
		return err
	}
	s.notify(listID)
	return nil
}

// RemoveEntry takes entry id off its list, or returns ErrNotFound.
func (s *Store) RemoveEntry(ctx context.Context, id int64) error {
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	var listID int64
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		var err error
		listID, err = q.DeleteEntry(ctx, shoppingdb.DeleteEntryParams{ID: id, HouseholdID: hid})
		return rowOrNotFound("entry", id, err)
	})
	if err != nil {
		return err
	}
	s.notify(listID)
	return nil
}
