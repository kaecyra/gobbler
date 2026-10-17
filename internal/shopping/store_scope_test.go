package shopping

import (
	"context"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/store"
)

// otherHousehold returns a context for a second household (B) in the fixture's
// database, with the identity of its member.
func (f *fixture) otherHousehold() context.Context {
	f.t.Helper()
	h, err := household.New(f.d).Create(f.ctx, "Other household", auth.DevOtherEmail)
	if err != nil {
		f.t.Fatalf("create second household: %v", err)
	}
	return household.WithContext(f.ctx, h)
}

// Household A owns a list with a recipe entry, a manual item and a check, and a
// pantry row. Every call as household B finds none of it, changes none of it
// and sees none of it (ADR-00018).
func TestOtherHouseholdCannotReachAnotherHouseholdsShopping(t *testing.T) {
	f := newFixture(t)
	f.makeCentral(f.salt) // B puts it in its own pantry
	b := f.otherHousehold()
	l := f.newList("Week")
	r := f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"))
	entry := f.addEntry(l, r, 1)
	manual, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Text: "napkins"})
	if err != nil {
		t.Fatal(err)
	}
	f.check(l, ingKey(f.flour), true)
	if err := f.s.AddToPantry(f.ctx, recipeID(f.salt)); err != nil {
		t.Fatal(err)
	}

	notFound := map[string]func() error{
		"GetList":            func() error { _, err := f.s.GetList(b, l.ID); return err },
		"Load":               func() error { _, err := f.s.Load(b, l.ID); return err },
		"Regenerate":         func() error { _, err := f.s.Regenerate(b, l.ID); return err },
		"RenameList":         func() error { return f.s.RenameList(b, l.ID, "Mine now") },
		"DeleteList":         func() error { return f.s.DeleteList(b, l.ID) },
		"SetIncludeOptional": func() error { return f.s.SetIncludeOptional(b, l.ID, true) },
		"AddEntry":           func() error { _, err := f.s.AddEntry(b, l.ID, r, quantity.Int(1)); return err },
		"SetEntryMultiplier": func() error { return f.s.SetEntryMultiplier(b, entry, quantity.Int(9)) },
		"RemoveEntry":        func() error { return f.s.RemoveEntry(b, entry) },
		"AddManualItem":      func() error { _, err := f.s.AddManualItem(b, l.ID, ManualItem{Text: "x"}); return err },
		"EditManualItem":     func() error { return f.s.EditManualItem(b, manual, ManualItem{Text: "x"}) },
		"RemoveManualItem":   func() error { return f.s.RemoveManualItem(b, manual) },
		"SetChecked on":      func() error { return f.s.SetChecked(b, l.ID, ingKey(f.egg), true) },
		"SetChecked off":     func() error { return f.s.SetChecked(b, l.ID, ingKey(f.flour), false) },
	}
	for name, call := range notFound {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s as the other household = %v, want ErrNotFound", name, err)
		}
	}

	// B sees no lists and an empty pantry; A's pantry is untouched by B's.
	if lists, err := f.s.Lists(b); err != nil || len(lists) != 0 {
		t.Errorf("other household's lists = %v (err %v), want none", lists, err)
	}
	if p, err := f.s.Pantry(b); err != nil || len(p) != 0 {
		t.Errorf("other household's pantry = %v (err %v), want empty", p, err)
	}
	if err := f.s.AddToPantry(b, recipeID(f.salt)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RemoveFromPantry(b, recipeID(f.salt)); err != nil {
		t.Fatal(err)
	}
	if p, err := f.s.Pantry(f.ctx); err != nil || len(p) != 1 {
		t.Errorf("A's pantry after B's changes = %v (err %v), want its one ingredient", p, err)
	}

	// B cannot put A's recipe on B's own list: the recipe is not B's.
	bl, err := f.s.CreateList(b, "B's week")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AddEntry(b, bl.ID, r, quantity.Int(1)); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("AddEntry of A's recipe on B's list = %v, want ErrUnknownReference", err)
	}

	// None of it changed A's list.
	v := f.load(l)
	if v.List.Name != "Week" || len(v.Entries) != 1 || quantity.EncodeRat(v.Entries[0].Multiplier) != quantity.EncodeRat(quantity.Int(1)) || len(v.Manual) != 1 || v.IncludeOptional {
		t.Errorf("A's list changed: %+v", v)
	}
	if got := f.count(`SELECT COUNT(*) FROM shopping_checks WHERE list_id = ?`, l.ID); got != 1 {
		t.Errorf("A's list has %d checks, want 1", got)
	}
}

// The pantry of a household decides what its own lists show and nobody else's.
func TestPantryIsPerHousehold(t *testing.T) {
	f := newFixture(t)
	f.makeCentral(f.flour) // household B uses it too
	b := f.otherHousehold()
	r := f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"))
	if err := f.s.AddToPantry(f.ctx, recipeID(f.flour)); err != nil {
		t.Fatal(err)
	}
	// B has the same recipe content of its own and its own list.
	rb, err := f.recipes.Create(b, recipeOf("Cake", f.ing(f.flour, 1, 1, "cup")))
	if err != nil {
		t.Fatal(err)
	}
	la, lb := f.newList("A"), f.listIn(b, "B")
	f.addEntry(la, r, 1)
	if _, err := f.s.AddEntry(b, lb.ID, rb, quantity.Int(1)); err != nil {
		t.Fatal(err)
	}
	if _, ok := viewItems(f.load(la))["zz test flour"]; ok {
		t.Error("A's pantry flour is still on A's list")
	}
	vb, err := f.s.Load(b, lb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := viewItems(vb)["zz test flour"]; !ok {
		t.Error("A's pantry hid flour on B's list")
	}
}

// listIn creates a list in the household of ctx.
func (f *fixture) listIn(ctx context.Context, name string) ListInfo {
	f.t.Helper()
	l, err := f.s.CreateList(ctx, name)
	if err != nil {
		f.t.Fatal(err)
	}
	return l
}

// The store never falls back to unscoped data: a context with no household is
// refused, on reads and writes alike.
func TestNoHouseholdIsRefused(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	bare := authedCtx(t)
	calls := map[string]func() error{
		"CreateList":  func() error { _, err := f.s.CreateList(bare, "x"); return err },
		"Lists":       func() error { _, err := f.s.Lists(bare); return err },
		"GetList":     func() error { _, err := f.s.GetList(bare, l.ID); return err },
		"Load":        func() error { _, err := f.s.Load(bare, l.ID); return err },
		"Regenerate":  func() error { _, err := f.s.Regenerate(bare, l.ID); return err },
		"RenameList":  func() error { return f.s.RenameList(bare, l.ID, "x") },
		"DeleteList":  func() error { return f.s.DeleteList(bare, l.ID) },
		"AddEntry":    func() error { _, err := f.s.AddEntry(bare, l.ID, 1, quantity.Int(1)); return err },
		"AddManual":   func() error { _, err := f.s.AddManualItem(bare, l.ID, ManualItem{Text: "x"}); return err },
		"SetChecked":  func() error { return f.s.SetChecked(bare, l.ID, ingKey(f.flour), true) },
		"Pantry":      func() error { _, err := f.s.Pantry(bare); return err },
		"AddToPantry": func() error { return f.s.AddToPantry(bare, recipeID(f.flour)) },
		"RemoveFrom":  func() error { return f.s.RemoveFromPantry(bare, recipeID(f.flour)) },
		"SetOptional": func() error { return f.s.SetIncludeOptional(bare, l.ID, true) },
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrNoHousehold) {
			t.Errorf("%s with no household = %v, want ErrNoHousehold", name, err)
		}
	}
}

// An ingredient of household A's overlay is not visible to household B
// (ADR-00018), so B cannot save a reference to it: not on a manual item, in
// the pantry or on a recipe line. The refusal is the one a missing id gets, and
// nothing of B's now names A's entry, so A's household can still be deleted.
func TestOtherHouseholdCannotReferenceAnotherHouseholdsIngredient(t *testing.T) {
	f := newFixture(t)
	b := f.otherHousehold()
	bl := f.listIn(b, "B")
	own, err := f.catalog.CreateIngredient(b, catalog.Ingredient{Name: "zz b only", Aisle: "x"})
	if err != nil {
		t.Fatal(err)
	}
	mine := f.flour // A's overlay entry

	if _, err := f.s.AddManualItem(b, bl.ID, ManualItem{Ingredient: recipeID(mine)}); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("manual item naming A's ingredient = %v, want ErrUnknownReference", err)
	}
	ok, err := f.s.AddManualItem(b, bl.ID, ManualItem{Ingredient: recipeID(own.ID)})
	if err != nil {
		t.Fatalf("manual item naming B's own ingredient: %v", err)
	}
	if err := f.s.EditManualItem(b, ok, ManualItem{Ingredient: recipeID(mine)}); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("editing a manual item to A's ingredient = %v, want ErrUnknownReference", err)
	}
	if err := f.s.AddToPantry(b, recipeID(mine)); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("pantry add of A's ingredient = %v, want ErrUnknownReference", err)
	}
	if err := f.s.AddToPantry(b, recipeID(own.ID)); err != nil {
		t.Errorf("pantry add of B's own ingredient: %v", err)
	}
	rec := recipeOf("Bread", f.ing(mine, 1, 1, "cup"))
	if _, err := f.recipes.Create(b, rec); !errors.Is(err, store.ErrUnknownReference) {
		t.Errorf("recipe line naming A's ingredient = %v, want store.ErrUnknownReference", err)
	}
	good, err := f.recipes.Create(b, recipeOf("Bread", f.ing(own.ID, 1, 1, "cup")))
	if err != nil {
		t.Fatalf("recipe line naming B's own ingredient: %v", err)
	}
	if err := f.recipes.Update(b, good, rec); !errors.Is(err, store.ErrUnknownReference) {
		t.Errorf("updating a recipe to name A's ingredient = %v, want store.ErrUnknownReference", err)
	}

	var aid int64
	if err := f.d.QueryRow(`SELECT household_id FROM ingredients WHERE id = ?`, mine).Scan(&aid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Exec(`DELETE FROM households WHERE id = ?`, aid); err != nil {
		t.Errorf("deleting A's household after B tried to reference its ingredient: %v", err)
	}
}
