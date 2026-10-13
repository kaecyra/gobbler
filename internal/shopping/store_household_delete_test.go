package shopping

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// Deleting a household leaves no shopping rows of it (ADR-00018): its lists,
// the entries naming its recipes, manual items, checks and pantry all go with
// it, and the recipes' foreign key from the entries does not stop the delete.
// Household deletion itself ships elsewhere; this is the schema obligation it
// relies on.
func TestDeletingAHouseholdRemovesItsShoppingRows(t *testing.T) {
	f := newFixture(t)
	f.makeCentral(f.flour, f.salt) // household B uses them too
	b := f.otherHousehold()

	// A second household's rows must survive the first one's deletion.
	rb, err := f.recipes.Create(b, recipeOf("Bread", f.ing(f.flour, 1, 1, "cup")))
	if err != nil {
		t.Fatal(err)
	}
	lb := f.listIn(b, "B")
	if _, err := f.s.AddEntry(b, lb.ID, rb, quantity.Int(1)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.AddToPantry(b, recipeID(f.salt)); err != nil {
		t.Fatal(err)
	}

	l := f.newList("Week")
	r1 := f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"))
	r2 := f.createRecipe("Pie", f.ing(f.butter, 1, 1, "cup"))
	f.addEntry(l, r1, 1)
	f.addEntry(l, r2, 2)
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Text: "napkins"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Ingredient: recipeID(f.egg)}); err != nil {
		t.Fatal(err)
	}
	f.check(l, ingKey(f.flour), true)
	if err := f.s.AddToPantry(f.ctx, recipeID(f.salt)); err != nil {
		t.Fatal(err)
	}

	var hid int64
	if err := f.d.QueryRow(`SELECT household_id FROM shopping_lists WHERE id = ?`, l.ID).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.Exec(`DELETE FROM households WHERE id = ?`, hid); err != nil {
		t.Fatalf("deleting a household that holds shopping rows: %v", err)
	}

	for _, table := range []string{"shopping_lists", "shopping_entries", "shopping_manual_items", "shopping_checks", "shopping_pantry", "recipes"} {
		if n := f.count(`SELECT COUNT(*) FROM `+table+` WHERE household_id = ?`, hid); n != 0 {
			t.Errorf("%s still has %d rows of the deleted household", table, n)
		}
	}
	for table, want := range map[string]int{"shopping_lists": 1, "shopping_entries": 1, "shopping_pantry": 1} {
		if n := f.count(`SELECT COUNT(*) FROM ` + table); n != want {
			t.Errorf("%s has %d rows after the delete, want the other household's %d", table, n, want)
		}
	}
	if n := f.count(`SELECT COUNT(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("foreign_key_check reports %d violations after the delete", n)
	}
}
