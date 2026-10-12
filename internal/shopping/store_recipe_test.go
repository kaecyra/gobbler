package shopping

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Deleting a recipe that a list uses is refused, and the list is untouched. The
// owner removes the entry first, or the list.
func TestDeletingARecipeOnAListFails(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	r := f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"))
	entry := f.addEntry(l, r, 1)

	err := f.recipes.Delete(f.ctx, r)
	if err == nil {
		t.Fatal("deleting a recipe that is on a list succeeded")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("err = %v, want the foreign key violation", err)
	}
	if _, ok := viewItems(f.load(l))["zz test flour"]; !ok || len(f.load(l).Entries) != 1 {
		t.Error("the list lost its entry")
	}

	if err := f.s.RemoveEntry(f.ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err := f.recipes.Delete(f.ctx, r); err != nil {
		t.Errorf("deleting a recipe no list uses: %v", err)
	}
}

// Merging a catalog ingredient re-points everything this package stores about
// it, checks included.
func TestIngredientMergeRepointsShoppingRows(t *testing.T) {
	f := newFixture(t)
	f.s.RegisterMergeHooks(f.catalog)
	f.recipes.RegisterMergeHooks(f.catalog)
	l := f.newList("L")
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Ingredient: recipeID(f.egg)}); err != nil {
		t.Fatal(err)
	}
	// Both are in the pantry, and both checked: the winner's rows must survive
	// the loser's.
	for _, id := range []int64{f.egg, f.butter} {
		if err := f.s.AddToPantry(f.ctx, recipeID(id)); err != nil {
			t.Fatal(err)
		}
		f.check(l, ingKey(id), true)
	}
	other := f.newList("Other")
	f.check(other, ingKey(f.egg), true)

	if _, err := f.catalog.MergeIngredients(f.ctx, f.butter, f.egg); err != nil {
		t.Fatalf("MergeIngredients: %v", err)
	}
	if n := f.count(`SELECT COUNT(*) FROM shopping_manual_items WHERE ingredient_id = ?`, f.butter); n != 1 {
		t.Errorf("manual items on the winner = %d, want 1", n)
	}
	if ids, _ := f.s.Pantry(f.ctx); len(ids) != 1 || ids[0] != recipeID(f.butter) {
		t.Errorf("pantry = %v, want the winner once", ids)
	}
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks WHERE item_key = ?`, string(ingKey(f.egg))); n != 0 {
		t.Errorf("checks on the merged-away ingredient = %d, want 0", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks WHERE item_key = ?`, string(ingKey(f.butter))); n != 2 {
		t.Errorf("checks on the winner = %d, want one per list", n)
	}
}

// Until RegisterMergeHooks is wired, a merge of an ingredient that shopping rows
// reference is refused by the foreign keys and rolled back whole.
func TestIngredientMergeWithoutShoppingHooksIsRefusedAndRolledBack(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	if _, err := f.s.AddManualItem(f.ctx, l.ID, ManualItem{Ingredient: recipeID(f.egg)}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.AddToPantry(f.ctx, recipeID(f.egg)); err != nil {
		t.Fatal(err)
	}
	f.check(l, ingKey(f.egg), true)

	if _, err := f.catalog.MergeIngredients(f.ctx, f.butter, f.egg); err == nil {
		t.Fatal("merge succeeded although shopping rows reference the loser and no hook is registered")
	}
	for _, id := range []int64{f.egg, f.butter} {
		if _, err := f.catalog.GetIngredient(f.ctx, id); err != nil {
			t.Errorf("ingredient %d after the refused merge: %v", id, err)
		}
	}
	// The catalog moved the loser's name to the winner before the failure; the
	// rollback must have undone that.
	if _, ok, err := f.catalog.MatchIngredient(f.ctx, "zz test egg"); err != nil || !ok {
		t.Errorf("loser name no longer matches: ok %v, err %v", ok, err)
	} else if got, _, _ := f.catalog.MatchIngredient(f.ctx, "zz test egg"); got.ID != f.egg {
		t.Errorf("loser name now matches ingredient %d, want %d", got.ID, f.egg)
	}
	if n := f.count(`SELECT COUNT(*) FROM shopping_manual_items WHERE ingredient_id = ?`, f.egg); n != 1 {
		t.Errorf("manual items on the loser = %d, want 1", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM shopping_pantry WHERE ingredient_id = ?`, f.egg); n != 1 {
		t.Errorf("pantry rows on the loser = %d, want 1", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM shopping_checks WHERE item_key = ?`, string(ingKey(f.egg))); n != 1 {
		t.Errorf("checks on the loser = %d, want 1", n)
	}
}

// The migration applies on a database that already holds catalog data and
// recipes, and leaves them alone.
func TestMigrationOnExistingData(t *testing.T) {
	f := newFixture(t)
	// A second database stopped before the shopping migration.
	older := fstest.MapFS{}
	all := db.Migrations()
	entries, err := fs.ReadDir(all, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "00005_shopping.sql" || e.IsDir() {
			continue
		}
		body, err := fs.ReadFile(all, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		older[e.Name()] = &fstest.MapFile{Data: body}
	}
	d2 := openRaw(t)
	if err := db.Migrate(f.ctx, d2, older); err != nil {
		t.Fatalf("migrate without shopping: %v", err)
	}
	ing := f.catalogOn(d2)
	id, err := ing.CreateIngredient(f.ctx, catalogIngredient("zz test existing"))
	if err != nil {
		t.Fatal(err)
	}
	rid, err := f.recipesOn(d2).Create(f.ctx, recipeOf("Old", line(recipe.IngredientID(id.ID), amt(t, 1, 1, "cup"))))
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Migrate(f.ctx, d2, all); err != nil {
		t.Fatalf("migrate with shopping on existing data: %v", err)
	}
	if _, err := f.recipesOn(d2).Get(f.ctx, rid); err != nil {
		t.Errorf("recipe after migration: %v", err)
	}
	s2 := New(d2, f.recipesOn(d2), ing)
	l, err := s2.CreateList(f.ctx, "L")
	if err != nil {
		t.Fatalf("CreateList after migration: %v", err)
	}
	if _, err := s2.AddEntry(f.ctx, l.ID, rid, quantity3(t)); err != nil {
		t.Errorf("AddEntry of an existing recipe: %v", err)
	}
}
