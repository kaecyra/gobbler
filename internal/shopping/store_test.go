package shopping

import (
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func TestListLifecycle(t *testing.T) {
	f := newFixture(t)
	a, b := f.newList("  Weekend "), f.newList("Party")
	if a.Name != "Weekend" {
		t.Errorf("name = %q, want it trimmed", a.Name)
	}
	if err := f.s.RenameList(f.ctx, a.ID, "Weekday"); err != nil {
		t.Fatal(err)
	}
	lists, err := f.s.Lists(f.ctx)
	if err != nil || len(lists) != 2 || lists[0].Name != "Weekday" || lists[1].ID != b.ID {
		t.Fatalf("Lists = %+v, %v", lists, err)
	}
	if err := f.s.DeleteList(f.ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Load(f.ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load deleted list: err = %v, want ErrNotFound", err)
	}
	if err := f.s.DeleteList(f.ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteList twice: err = %v, want ErrNotFound", err)
	}
	if err := f.s.RenameList(f.ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RenameList missing: err = %v, want ErrNotFound", err)
	}
	if _, err := f.s.CreateList(f.ctx, "  "); !errors.Is(err, ErrInvalid) {
		t.Errorf("CreateList blank: err = %v, want ErrInvalid", err)
	}
	if err := f.s.RenameList(f.ctx, b.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("RenameList blank: err = %v, want ErrInvalid", err)
	}
}

func TestDeleteListRemovesItsContents(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	f.addEntry(l, f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup")), 1)
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Text: "napkins"}); err != nil {
		t.Fatal(err)
	}
	f.check(l, ingKey(f.flour), true)
	if err := f.s.DeleteList(f.ctx, l.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"shopping_entries", "shopping_manual_items", "shopping_checks"} {
		if n := f.count(`SELECT COUNT(*) FROM ` + table); n != 0 {
			t.Errorf("%s keeps %d rows of a deleted list", table, n)
		}
	}
}

func TestLoadGeneratesFromStoredEntries(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	cake := f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"), f.ing(f.butter, 2, 1, "tbsp"))
	bread := f.createRecipe("Bread", f.ing(f.flour, 2, 1, "cup"))
	cakeEntry := f.addEntry(l, cake, 2)
	f.addEntry(l, bread, 1)

	v := f.load(l)
	flour := viewItems(v)["zz test flour"]
	wantAmounts(t, flour.Item, "4 cup") // 1 cup x 2 plus 2 cup
	if len(flour.Sources) != 2 {
		t.Errorf("flour sources = %v, want both recipes", flour.Sources)
	}
	if len(v.Entries) != 2 || v.Entries[0].Name != "Cake" || v.Entries[0].Multiplier.String() != "2" {
		t.Errorf("entries = %+v", v.Entries)
	}
	if v.Aisles[0].Name != "baking" || v.Aisles[1].Name != "dairy" {
		t.Errorf("aisles = %+v, want baking then dairy", v.Aisles)
	}

	if err := f.s.SetEntryMultiplier(f.ctx, cakeEntry, rat(t, 1, 2)); err != nil {
		t.Fatal(err)
	}
	wantAmounts(t, viewItems(f.load(l))["zz test flour"].Item, "2 1/2 cup")

	if err := f.s.RemoveEntry(f.ctx, cakeEntry); err != nil {
		t.Fatal(err)
	}
	if _, ok := viewItems(f.load(l))["zz test butter"]; ok {
		t.Error("butter still listed after its only recipe was removed")
	}
}

func TestEntryErrors(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	r := f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"))
	e := f.addEntry(l, r, 1)

	if _, err := f.s.AddEntry(f.ctx, l.ID, r, quantity.Int(2)); !errors.Is(err, ErrDuplicate) {
		t.Errorf("same recipe twice: err = %v, want ErrDuplicate", err)
	}
	if _, err := f.s.AddEntry(f.ctx, l.ID, 999, quantity.Int(1)); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("unknown recipe: err = %v, want ErrUnknownReference", err)
	}
	if _, err := f.s.AddEntry(f.ctx, 999, r, quantity.Int(1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing list: err = %v, want ErrNotFound", err)
	}
	other := f.createRecipe("Pie", f.ing(f.flour, 1, 1, "cup"))
	if _, err := f.s.AddEntry(f.ctx, l.ID, other, quantity.Int(0)); !errors.Is(err, ErrInvalid) {
		t.Errorf("zero multiplier: err = %v, want ErrInvalid", err)
	}
	if err := f.s.SetEntryMultiplier(f.ctx, e, quantity.Int(-1)); !errors.Is(err, ErrInvalid) {
		t.Errorf("negative multiplier: err = %v, want ErrInvalid", err)
	}
	if err := f.s.SetEntryMultiplier(f.ctx, 999, quantity.Int(1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing entry: err = %v, want ErrNotFound", err)
	}
	if err := f.s.RemoveEntry(f.ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove missing entry: err = %v, want ErrNotFound", err)
	}
	if got := viewItems(f.load(l))["zz test flour"]; got.Amounts[0].String() != "1 cup" {
		t.Errorf("a refused write changed the list: flour = %v", got.Amounts)
	}
}

func TestManualItemsRoundTrip(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	rng, err := quantity.NewRange(rat(t, 1, 2), rat(t, 3, 4))
	if err != nil {
		t.Fatal(err)
	}
	canned := quantity.Amount{
		Qty: quantity.Exact(quantity.Int(2)), Unit: quantity.OtherUnit("can"),
		Package: quantity.PackageSize{Qty: quantity.Exact(quantity.Int(14)), Unit: quantity.MustUnit("oz")}, HasPackage: true,
	}
	text, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Text: " Paper Towels "})
	if err != nil {
		t.Fatal(err)
	}
	butter, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{
		Ingredient: recipe.IngredientID(f.butter), Amount: quantity.Amount{Qty: rng, Unit: quantity.MustUnit("cup")},
	})
	if err != nil {
		t.Fatal(err)
	}
	can, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Ingredient: recipe.IngredientID(f.egg), Text: "beans", Amount: canned})
	if err != nil {
		t.Fatal(err)
	}

	v := f.load(l)
	if len(v.Manual) != 3 || v.Manual[0].ID != text || v.Manual[0].Text != "Paper Towels" {
		t.Fatalf("manual = %+v", v.Manual)
	}
	got := v.Manual[1].Amount
	if got.Qty.String() != "1/2-3/4" || got.Unit.Name != "cup" {
		t.Errorf("range amount = %v, want 1/2-3/4 cup", got)
	}
	if a := v.Manual[2].Amount; !a.HasPackage || a.Package.Unit.Name != "oz" || a.Package.Qty.String() != "14" || a.Unit.Name != "can" {
		t.Errorf("packaged amount = %+v, want 2 (14 oz) can", a)
	}
	if it := viewItems(v)["Paper Towels"]; !it.Manual || it.Key != "t:paper towels" {
		t.Errorf("free-text item = %+v", it)
	}

	if err := f.s.EditManualItem(f.ctx, butter, ManualItem{Ingredient: recipe.IngredientID(f.butter), Amount: amt(t, 3, 1, "tbsp")}); err != nil {
		t.Fatal(err)
	}
	wantAmounts(t, viewItems(f.load(l))["zz test butter"].Item, "3 tbsp")
	if err := f.s.RemoveManualItem(f.ctx, can); err != nil {
		t.Fatal(err)
	}
	if n := len(f.load(l).Manual); n != 2 {
		t.Errorf("manual items after remove = %d, want 2", n)
	}
}

func TestManualItemErrors(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	id, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Text: "milk"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Text: " "}); !errors.Is(err, ErrInvalidManualItem) || !errors.Is(err, ErrInvalid) {
		t.Errorf("empty item: err = %v, want ErrInvalid and ErrInvalidManualItem", err)
	}
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Ingredient: 99999}); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("unknown ingredient: err = %v, want ErrUnknownReference", err)
	}
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Text: "x", Amount: quantity.Amount{Unit: quantity.OtherUnit("zz-no-such-unit")}}); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("unknown unit: err = %v, want ErrUnknownReference", err)
	}
	if _, err := f.s.AddManualItem(f.ctx, 999, ManualItem{Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing list: err = %v, want ErrNotFound", err)
	}
	if err := f.s.EditManualItem(f.ctx, 999, ManualItem{Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("edit missing: err = %v, want ErrNotFound", err)
	}
	if err := f.s.EditManualItem(f.ctx, id, ManualItem{Ingredient: 99999}); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("edit to unknown ingredient: err = %v, want ErrUnknownReference", err)
	}
	if err := f.s.RemoveManualItem(f.ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove missing: err = %v, want ErrNotFound", err)
	}
}

func TestPantryIsIdempotentAndSetsItemsAside(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	f.addEntry(l, f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"), f.ing(f.salt, 1, 1, "tsp")), 1)

	for i := 0; i < 2; i++ {
		if err := f.s.AddToPantry(f.ctx, recipe.IngredientID(f.salt)); err != nil {
			t.Fatalf("AddToPantry #%d: %v", i+1, err)
		}
	}
	if ids, err := f.s.Pantry(f.ctx); err != nil || len(ids) != 1 || ids[0] != recipe.IngredientID(f.salt) {
		t.Fatalf("Pantry = %v, %v; want salt once", ids, err)
	}
	v := f.load(l)
	if _, ok := viewItems(v)["zz test salt"]; ok {
		t.Error("pantry ingredient is still on the list")
	}
	if len(v.Pantry) != 1 || v.Pantry[0].Name != "zz test salt" {
		t.Errorf("set-aside pantry items = %+v, want salt", v.Pantry)
	}

	// A manual item naming a pantry ingredient puts it back on the list.
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Ingredient: recipe.IngredientID(f.salt)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := viewItems(f.load(l))["zz test salt"]; !ok {
		t.Error("manual item did not add the pantry ingredient back")
	}

	for i := 0; i < 2; i++ {
		if err := f.s.RemoveFromPantry(f.ctx, recipe.IngredientID(f.salt)); err != nil {
			t.Fatalf("RemoveFromPantry #%d: %v", i+1, err)
		}
	}
	if ids, _ := f.s.Pantry(f.ctx); len(ids) != 0 {
		t.Errorf("Pantry = %v, want empty", ids)
	}
	if err := f.s.AddToPantry(f.ctx, 99999); !errors.Is(err, ErrUnknownReference) {
		t.Errorf("unknown ingredient: err = %v, want ErrUnknownReference", err)
	}
}
