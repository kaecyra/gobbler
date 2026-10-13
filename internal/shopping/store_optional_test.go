package shopping

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/db"
)

// optionalCake is flour (required) and butter (optional).
func (f *fixture) optionalCake() (ListInfo, int64) {
	f.t.Helper()
	butter := f.ing(f.butter, 2, 1, "tbsp")
	butter.Optional = true
	r := f.createRecipe("Cake", f.ing(f.flour, 1, 1, "cup"), butter)
	l := f.newList("L")
	f.addEntry(l, r, 1)
	return l, r
}

func (f *fixture) setOptional(l ListInfo, on bool) {
	f.t.Helper()
	if err := f.s.SetIncludeOptional(f.ctx, l.ID, on); err != nil {
		f.t.Fatalf("SetIncludeOptional(%v): %v", on, err)
	}
}

func TestIncludeOptionalDefaultsOffAndRoundTrips(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	if f.load(l).IncludeOptional {
		t.Fatal("a new list includes optional lines")
	}
	f.setOptional(l, true)
	if !f.load(l).IncludeOptional || !f.regenerate(l).IncludeOptional {
		t.Fatal("the flag did not round-trip through Load and Regenerate")
	}
	f.setOptional(l, false)
	if f.load(l).IncludeOptional {
		t.Fatal("the flag did not clear")
	}
}

func TestToggleAddsAndRemovesOptionalLines(t *testing.T) {
	f := newFixture(t)
	l, _ := f.optionalCake()
	if _, ok := viewItems(f.load(l))["zz test butter"]; ok {
		t.Fatal("optional butter is on a list that does not include optional lines")
	}
	f.setOptional(l, true)
	items := viewItems(f.load(l))
	if _, ok := items["zz test butter"]; !ok {
		t.Fatal("optional butter missing after including optional lines")
	}
	wantAmounts(t, items["zz test butter"].Item, "2 tbsp")
	f.setOptional(l, false)
	if _, ok := viewItems(f.load(l))["zz test butter"]; ok {
		t.Fatal("optional butter still listed after excluding optional lines")
	}
}

func TestChecksSurviveTheToggle(t *testing.T) {
	f := newFixture(t)
	l, _ := f.optionalCake()
	f.setOptional(l, true)
	f.check(l, ingKey(f.flour), true)
	f.check(l, ingKey(f.butter), true)

	f.setOptional(l, false)
	v := f.load(l)
	if !viewItems(v)["zz test flour"].Checked {
		t.Error("flour lost its check when optional lines were excluded")
	}
	// Excluding does not discard the optional item's check; Load only ignores it.
	f.setOptional(l, true)
	if !viewItems(f.load(l))["zz test butter"].Checked {
		t.Error("butter came back unchecked after the toggle")
	}
	if !viewItems(f.regenerate(l))["zz test flour"].Checked {
		t.Error("flour lost its check on Regenerate")
	}

	// Regenerate with the lines excluded drops the check of an item that is gone.
	f.setOptional(l, false)
	f.regenerate(l)
	f.setOptional(l, true)
	if viewItems(f.load(l))["zz test butter"].Checked {
		t.Error("butter kept its check through a Regenerate that dropped it")
	}
}

func TestSetIncludeOptionalNotifiesOnlyOnChange(t *testing.T) {
	f := newFixture(t)
	l := f.newList("L")
	var got []int64
	f.s.OnChange(func(c Change) { got = append(got, c.ListID) })
	f.setOptional(l, false) // already off
	if len(got) != 0 {
		t.Fatalf("notified %v for a no-op", got)
	}
	f.setOptional(l, true)
	f.setOptional(l, true)
	if len(got) != 1 || got[0] != l.ID {
		t.Fatalf("notifications = %v, want one for list %d", got, l.ID)
	}
	if err := f.s.SetIncludeOptional(f.ctx, 9999, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing list: err = %v, want ErrNotFound", err)
	}
}

// migrationsBefore returns the embedded migrations that sort before the file
// whose name starts with first, which is excluded along with every later one.
func migrationsBefore(t *testing.T, first string) fs.FS {
	t.Helper()
	older := fstest.MapFS{}
	all := db.Migrations()
	entries, err := fs.ReadDir(all, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() >= first {
			continue
		}
		body, err := fs.ReadFile(all, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		older[e.Name()] = &fstest.MapFile{Data: body}
	}
	return older
}

// Lists made before the flag existed keep their entries and keep dropping
// optional lines.
func TestIncludeOptionalMigrationOnExistingLists(t *testing.T) {
	f := newFixture(t)
	d2 := openRaw(t)
	if err := db.Migrate(f.ctx, d2, migrationsBefore(t, "00006")); err != nil {
		t.Fatalf("migrate to the shopping tables: %v", err)
	}
	cat, rec := f.catalogOn(d2), f.recipesOn(d2)
	ctx := f.ctx
	flour, err := cat.CreateIngredient(f.ctx, catalog.Ingredient{Name: "zz old flour", Aisle: "baking"})
	if err != nil {
		t.Fatal(err)
	}
	butter, err := cat.CreateIngredient(f.ctx, catalog.Ingredient{Name: "zz old butter", Aisle: "dairy"})
	if err != nil {
		t.Fatal(err)
	}
	rid := oldRecipe(t, d2, "Old", oldLine{ingredient: flour.ID}, oldLine{ingredient: butter.ID, optional: true})
	for _, q := range []string{
		`INSERT INTO shopping_lists (id, name, created_at) VALUES (7, 'Old list', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := d2.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d2.Exec(`INSERT INTO shopping_entries (list_id, recipe_id, multiplier) VALUES (7, ?, '1/1')`, rid); err != nil {
		t.Fatal(err)
	}

	ctx = migrateOverOldData(ctx, t, d2)
	s2 := New(d2, rec, cat)
	v, err := s2.Load(ctx, 7)
	if err != nil {
		t.Fatalf("Load of a list that predates the flag: %v", err)
	}
	if v.IncludeOptional || len(v.Entries) != 1 {
		t.Fatalf("old list: include optional %v, entries %d", v.IncludeOptional, len(v.Entries))
	}
	items := viewItems(v)
	if _, ok := items["zz old flour"]; !ok {
		t.Error("required line missing")
	}
	if _, ok := items["zz old butter"]; ok {
		t.Error("an old list started including optional lines")
	}
}
