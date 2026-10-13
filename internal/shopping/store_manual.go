package shopping

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/shopping/shoppingdb"
)

// clean validates a manual item and trims its text.
func (m ManualItem) clean() (ManualItem, error) {
	m.Text = strings.TrimSpace(m.Text)
	if m.Text == "" && m.Ingredient == 0 {
		return m, fmt.Errorf("%w: %w", ErrInvalid, ErrInvalidManualItem)
	}
	return m, nil
}

func ingredientCol(m ManualItem) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(m.Ingredient), Valid: m.Ingredient != 0}
}

// AddManualItem adds a hand-written item to list listID and returns its id. It
// returns ErrInvalid for an item with neither text nor an ingredient, ErrNotFound
// for a missing list and ErrUnknownReference for an unknown ingredient or unit.
func (s *Store) AddManualItem(ctx context.Context, listID int64, m ManualItem) (int64, error) {
	m, err := m.clean()
	if err != nil {
		return 0, err
	}
	hid, err := scope(ctx)
	if err != nil {
		return 0, err
	}
	units, err := s.loadUnits(ctx)
	if err != nil {
		return 0, err
	}
	cols, err := units.encodeAmount(m.Amount)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		if err := lockList(ctx, q, hid, listID); err != nil {
			return err
		}
		var err error
		if err := requireVisible(ctx, q, hid, m.Ingredient); err != nil {
			return err
		}
		id, err = q.InsertManualItem(ctx, shoppingdb.InsertManualItemParams{
			HouseholdID: hid, ListID: listID, Text: m.Text, IngredientID: ingredientCol(m),
			Qty: cols.qty, UnitID: cols.unit, HasPackage: cols.hasPackage, PackageQty: cols.pkgQty, PackageUnitID: cols.pkgUnit,
		})
		return wrapWrite(fmt.Sprintf("add manual item to list %d", listID), err)
	})
	if err != nil {
		return 0, err
	}
	s.notify(listID)
	return id, nil
}

// EditManualItem replaces manual item id, or returns ErrNotFound, ErrInvalid or
// ErrUnknownReference.
func (s *Store) EditManualItem(ctx context.Context, id int64, m ManualItem) error {
	m, err := m.clean()
	if err != nil {
		return err
	}
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	units, err := s.loadUnits(ctx)
	if err != nil {
		return err
	}
	cols, err := units.encodeAmount(m.Amount)
	if err != nil {
		return err
	}
	var listID int64
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		var err error
		if err := requireVisible(ctx, q, hid, m.Ingredient); err != nil {
			return err
		}
		listID, err = q.UpdateManualItem(ctx, shoppingdb.UpdateManualItemParams{
			Text: m.Text, IngredientID: ingredientCol(m),
			Qty: cols.qty, UnitID: cols.unit, HasPackage: cols.hasPackage, PackageQty: cols.pkgQty, PackageUnitID: cols.pkgUnit,
			ID: id, HouseholdID: hid,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: manual item %d", ErrNotFound, id)
		}
		return wrapWrite(fmt.Sprintf("edit manual item %d", id), err)
	})
	if err != nil {
		return err
	}
	s.notify(listID)
	return nil
}

// RemoveManualItem deletes manual item id, or returns ErrNotFound.
func (s *Store) RemoveManualItem(ctx context.Context, id int64) error {
	hid, err := scope(ctx)
	if err != nil {
		return err
	}
	var listID int64
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		var err error
		listID, err = q.DeleteManualItem(ctx, shoppingdb.DeleteManualItemParams{ID: id, HouseholdID: hid})
		return rowOrNotFound("manual item", id, err)
	})
	if err != nil {
		return err
	}
	s.notify(listID)
	return nil
}

// requireVisible refuses an ingredient the household may not reference: one
// that is neither central nor in its own overlay (ADR-00018). It runs in the
// write's transaction and answers as a missing id does, ErrUnknownReference, so
// the response says nothing about other households. The zero id (a free-text
// item) is nothing to check.
func requireVisible(ctx context.Context, q *shoppingdb.Queries, hid int64, ingredient recipe.IngredientID) error {
	if ingredient == 0 {
		return nil
	}
	ok, err := q.IngredientVisible(ctx, shoppingdb.IngredientVisibleParams{ID: int64(ingredient), Scope: sql.NullInt64{Int64: hid, Valid: true}})
	if err != nil {
		return fmt.Errorf("read ingredient %d: %w", ingredient, err)
	}
	if !ok {
		return fmt.Errorf("ingredient %d: %w", ingredient, ErrUnknownReference)
	}
	return nil
}
