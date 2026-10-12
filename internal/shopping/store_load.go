package shopping

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/shopping/shoppingdb"
)

// EntryView is a recipe entry as stored, with the recipe's name.
type EntryView struct {
	ID         int64
	RecipeID   int64
	Name       string
	Multiplier quantity.Rat
}

// ManualView is a stored manual item with its id.
type ManualView struct {
	ID int64
	ManualItem
}

// View is a list as the live view and the export need it: what was stored and
// what Generate made of it. Aisles carries each item's key and check state;
// Pantry holds the items set aside because their ingredient is in the pantry.
type View struct {
	List    ListInfo
	Entries []EntryView
	Manual  []ManualView
	Aisles  []ViewAisle
	Pantry  []Item
}

// Generated returns the view's generated list without the view-only fields, for
// List.Text and the other consumers of Generate's result.
func (v View) Generated() List {
	l := List{Pantry: v.Pantry}
	for _, a := range v.Aisles {
		aisle := Aisle{Name: a.Name}
		for _, it := range a.Items {
			aisle.Items = append(aisle.Items, it.Item)
		}
		l.Aisles = append(l.Aisles, aisle)
	}
	return l
}

// snapshot is one consistent read of a list's stored rows.
type snapshot struct {
	list    shoppingdb.ShoppingList
	entries []shoppingdb.ListEntriesRow
	manual  []shoppingdb.ListManualItemsRow
	pantry  []int64
	checked []string
}

func readSnapshot(ctx context.Context, q *shoppingdb.Queries, id int64) (snapshot, error) {
	var s snapshot
	var err error
	if s.list, err = q.GetList(ctx, id); errors.Is(err, sql.ErrNoRows) {
		return s, fmt.Errorf("%w: list %d", ErrNotFound, id)
	} else if err != nil {
		return s, fmt.Errorf("read list %d: %w", id, err)
	}
	if s.entries, err = q.ListEntries(ctx, id); err != nil {
		return s, fmt.Errorf("read entries of list %d: %w", id, err)
	}
	if s.manual, err = q.ListManualItems(ctx, id); err != nil {
		return s, fmt.Errorf("read manual items of list %d: %w", id, err)
	}
	if s.pantry, err = q.ListPantry(ctx); err != nil {
		return s, fmt.Errorf("read pantry: %w", err)
	}
	if s.checked, err = q.ListChecked(ctx, id); err != nil {
		return s, fmt.Errorf("read checks of list %d: %w", id, err)
	}
	return s, nil
}

// Load returns list id with its generated items. It builds the items by
// calling Generate on the stored entries, manual items and pantry, and marks
// the ones that are checked. It writes nothing: check state of items that no
// longer exist is ignored here and dropped by Regenerate.
func (s *Store) Load(ctx context.Context, id int64) (View, error) {
	units, err := s.loadUnits(ctx)
	if err != nil {
		return View{}, err
	}
	var snap snapshot
	err = db.WithReadTx(ctx, s.db, s.q, func(q *shoppingdb.Queries) (err error) {
		snap, err = readSnapshot(ctx, q, id)
		return err
	})
	if err != nil {
		return View{}, err
	}
	v, _, err := s.build(ctx, snap, units)
	return v, err
}

// Regenerate is Load that also drops the check state of items that no longer
// exist, in the same transaction, so a later edit that brings an item back
// starts it unchecked. Items that still exist keep their checks, whatever
// happened to their amounts.
func (s *Store) Regenerate(ctx context.Context, id int64) (View, error) {
	units, err := s.loadUnits(ctx)
	if err != nil {
		return View{}, err
	}
	var view View
	var dropped int
	err = s.withTx(ctx, func(q *shoppingdb.Queries) error {
		if err := lockList(ctx, q, id); err != nil {
			return err
		}
		snap, err := readSnapshot(ctx, q, id)
		if err != nil {
			return err
		}
		var stale []ItemKey
		if view, stale, err = s.build(ctx, snap, units); err != nil {
			return err
		}
		for _, k := range stale {
			if _, err := q.ClearChecked(ctx, shoppingdb.ClearCheckedParams{ListID: id, ItemKey: string(k)}); err != nil {
				return fmt.Errorf("drop check %q of list %d: %w", k, id, err)
			}
		}
		dropped = len(stale)
		return nil
	})
	if err != nil {
		return View{}, err
	}
	if dropped > 0 {
		s.notify(id)
	}
	return view, nil
}

// build turns a snapshot into a view and the stored check keys naming no item.
func (s *Store) build(ctx context.Context, snap snapshot, units *unitTable) (View, []ItemKey, error) {
	info, err := listInfoFrom(snap.list)
	if err != nil {
		return View{}, nil, err
	}
	view := View{List: info}
	in := Input{Catalog: map[recipe.IngredientID]IngredientInfo{}, Pantry: map[recipe.IngredientID]bool{}}
	need := map[recipe.IngredientID]bool{}

	for _, e := range snap.entries {
		mult, err := decodeMultiplier(e.Multiplier)
		if err != nil {
			return View{}, nil, err
		}
		r, err := s.recipes.Get(ctx, e.RecipeID)
		if err != nil {
			return View{}, nil, fmt.Errorf("load recipe %d of list %d: %w", e.RecipeID, info.ID, err)
		}
		in.Entries = append(in.Entries, RecipeEntry{ID: e.RecipeID, Recipe: r, Multiplier: mult})
		view.Entries = append(view.Entries, EntryView{ID: e.ID, RecipeID: e.RecipeID, Name: r.Name, Multiplier: mult})
		for _, c := range r.Components {
			for _, l := range c.Lines {
				if l.Ingredient != 0 {
					need[l.Ingredient] = true
				}
			}
		}
	}
	for _, m := range snap.manual {
		amount, err := units.decodeAmount(amountCols{
			qty: m.Qty, unit: m.UnitID, hasPackage: m.HasPackage, pkgQty: m.PackageQty, pkgUnit: m.PackageUnitID,
		})
		if err != nil {
			return View{}, nil, fmt.Errorf("manual item %d: %w", m.ID, err)
		}
		item := ManualItem{Text: m.Text, Ingredient: recipe.IngredientID(m.IngredientID.Int64), Amount: amount}
		in.Manual = append(in.Manual, item)
		view.Manual = append(view.Manual, ManualView{ID: m.ID, ManualItem: item})
		if item.Ingredient != 0 {
			need[item.Ingredient] = true
		}
	}
	for _, id := range snap.pantry {
		in.Pantry[recipe.IngredientID(id)] = true
	}
	for id := range need {
		ing, err := s.catalog.GetIngredient(ctx, int64(id))
		if err != nil {
			return View{}, nil, fmt.Errorf("load ingredient %d of list %d: %w", id, info.ID, err)
		}
		in.Catalog[id] = IngredientInfo{Name: ing.Name, Aisle: ing.Aisle, DensityGPerML: ing.DensityGPerML}
	}

	generated, err := Generate(in)
	if err != nil {
		return View{}, nil, fmt.Errorf("generate list %d: %w", info.ID, err)
	}
	stored := make([]ItemKey, len(snap.checked))
	checked := make(map[ItemKey]bool, len(snap.checked))
	for i, k := range snap.checked {
		stored[i] = ItemKey(k)
		checked[ItemKey(k)] = true
	}
	view.Aisles = annotate(generated, checked)
	view.Pantry = generated.Pantry
	return view, staleKeys(generated, stored), nil
}
