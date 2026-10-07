package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

func TestIngredientCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	in := Ingredient{Name: "  ZZ Test Flour ", Aisle: "Baking", Aliases: []string{"ZZ Test Meal", "zz test meal", ""}, DensityGPerML: f64(0.5), USDAID: i64(168936)}
	got, err := s.CreateIngredient(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == 0 || got.Name != "zz test flour" || got.Aisle != "baking" || len(got.Aliases) != 1 || got.Aliases[0] != "zz test meal" {
		t.Fatalf("created = %+v, want normalized name, aisle and de-duplicated alias", got)
	}

	read, err := s.GetIngredient(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Reviewed || read.DensityGPerML == nil || *read.DensityGPerML != 0.5 || read.USDAID == nil || *read.USDAID != 168936 {
		t.Errorf("read = %+v", read)
	}

	unrev, err := s.ListUnreviewedIngredients(ctx)
	if err != nil || len(unrev) != 1 || unrev[0].ID != got.ID {
		t.Fatalf("ListUnreviewedIngredients = %+v err %v, want only the new ingredient", unrev, err)
	}

	read.Reviewed, read.Aisle, read.DensityGPerML, read.Aliases = true, "pantry", nil, []string{"zz test meal", "zz test powder"}
	if err := s.UpdateIngredient(ctx, read); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetIngredient(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Reviewed || after.Aisle != "pantry" || after.DensityGPerML != nil || len(after.Aliases) != 2 {
		t.Errorf("after update = %+v", after)
	}
	unrev, err = s.ListUnreviewedIngredients(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrev) != 0 {
		t.Errorf("reviewed ingredient still listed unreviewed: %+v", unrev)
	}

	all, err := s.ListIngredients(ctx)
	if err != nil || len(all) == 0 {
		t.Fatalf("ListIngredients = %d rows err %v", len(all), err)
	}
}

func TestIngredientErrors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.GetIngredient(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: %v, want ErrNotFound", err)
	}
	if err := s.UpdateIngredient(ctx, Ingredient{ID: 999999, Name: "zz test x", Aisle: "a"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update missing: %v, want ErrNotFound", err)
	}

	invalid := []Ingredient{
		{Name: "", Aisle: "produce"},
		{Name: "butter, melted", Aisle: "dairy"}, // qualifier belongs on the line, not the name
		{Name: "zz test y", Aisle: "  "},
		{Name: "zz test y", Aisle: "produce", DensityGPerML: f64(0)},
		{Name: "zz test y", Aisle: "produce", DensityGPerML: f64(-1)},
		{Name: "zz test y", Aisle: "produce", USDAID: i64(0)},
	}
	for _, in := range invalid {
		if _, err := s.CreateIngredient(ctx, in); !errors.Is(err, ErrInvalid) {
			t.Errorf("Create(%+v) = %v, want ErrInvalid", in, err)
		}
	}

	a := mustIngredient(t, s, Ingredient{Name: "zz test a", Aisle: "produce", Aliases: []string{"zz test alpha"}, USDAID: i64(7)})
	conflicts := []Ingredient{
		{Name: "zz test a", Aisle: "produce"},                                     // same name
		{Name: "zz test alpha", Aisle: "produce"},                                 // name equals an alias
		{Name: "zz test b", Aisle: "produce", Aliases: []string{"ZZ Test A"}},     // alias equals a name
		{Name: "zz test c", Aisle: "produce", Aliases: []string{"zz test alpha"}}, // alias equals an alias
		{Name: "zz test d", Aisle: "produce", USDAID: i64(7)},                     // duplicate USDA id
	}
	for _, in := range conflicts {
		if _, err := s.CreateIngredient(ctx, in); !errors.Is(err, ErrConflict) {
			t.Errorf("Create(%+v) = %v, want ErrConflict", in, err)
		}
	}
	// A failed create leaves nothing behind, aliases included.
	if _, ok, err := s.MatchIngredient(ctx, "zz test c"); err != nil || ok {
		t.Errorf("conflicting create left a row behind (ok=%v err=%v)", ok, err)
	}
	// An ingredient may update to keep its own name and aliases.
	if err := s.UpdateIngredient(ctx, a); err != nil {
		t.Errorf("Update unchanged: %v", err)
	}
}

func TestUnitCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	third, err := quantity.NewRat(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.CreateUnit(ctx, Unit{
		Unit:    quantity.Unit{Name: "ZZ Test Scoop", System: quantity.SystemUS, Dimension: quantity.DimVolume, Factor: third},
		Aliases: []string{"zz test scoops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.GetUnit(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Unit.Name != "zz test scoop" || !read.Unit.Factor.Equal(third) || read.Reviewed {
		t.Errorf("read = %+v, want exact 1/3 factor, unreviewed", read)
	}

	other, err := s.CreateUnit(ctx, Unit{Unit: quantity.OtherUnit("zz test glug"), Reviewed: true})
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.GetUnit(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o.Unit.Converts() || !o.Reviewed {
		t.Errorf("other unit = %+v", o)
	}

	unrev, err := s.ListUnreviewedUnits(ctx)
	if err != nil || len(unrev) != 1 || unrev[0].ID != got.ID {
		t.Fatalf("ListUnreviewedUnits = %+v err %v", unrev, err)
	}
	read.Reviewed = true
	read.Aliases = []string{"zz test scoops", "zz test scp"}
	if err := s.UpdateUnit(ctx, read); err != nil {
		t.Fatal(err)
	}
	u, err := s.GetUnit(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !u.Reviewed || len(u.Aliases) != 2 {
		t.Errorf("after update = %+v", u)
	}
	if all, err := s.ListUnits(ctx); err != nil || len(all) == 0 {
		t.Fatalf("ListUnits: %d rows, %v", len(all), err)
	}
}

func TestUnitErrors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.GetUnit(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: %v", err)
	}
	if err := s.UpdateUnit(ctx, Unit{ID: 999999, Unit: quantity.OtherUnit("zz test u")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update missing: %v", err)
	}
	bad := []Unit{
		{Unit: quantity.Unit{Name: "zz test u", System: quantity.SystemUS, Dimension: quantity.DimVolume}}, // no factor
		{Unit: quantity.Unit{Name: "zz test u", System: "imperial", Dimension: quantity.DimOther}},
		{Unit: quantity.Unit{Name: "zz test u", System: quantity.SystemUS, Dimension: "length"}},
		{Unit: quantity.OtherUnit("")},
	}
	for _, u := range bad {
		if _, err := s.CreateUnit(ctx, u); !errors.Is(err, ErrInvalid) {
			t.Errorf("CreateUnit(%+v) = %v, want ErrInvalid", u.Unit, err)
		}
	}
	if _, err := s.CreateUnit(ctx, Unit{Unit: quantity.OtherUnit("pinch")}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate unit name: %v, want ErrConflict", err)
	}
	if _, err := s.CreateUnit(ctx, Unit{Unit: quantity.OtherUnit("zz test u"), Aliases: []string{"tablespoons"}}); !errors.Is(err, ErrConflict) {
		t.Errorf("alias taken by seeded unit: %v, want ErrConflict", err)
	}
}

func TestEquipmentCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	got, err := s.CreateEquipment(ctx, Equipment{Name: "ZZ Test Mandoline", Aliases: []string{"zz test slicer"}})
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.GetEquipment(ctx, got.ID)
	if err != nil || read.Name != "zz test mandoline" || len(read.Aliases) != 1 || read.Reviewed {
		t.Fatalf("read = %+v err %v", read, err)
	}
	unrev, err := s.ListUnreviewedEquipment(ctx)
	if err != nil || len(unrev) != 1 {
		t.Fatalf("ListUnreviewedEquipment = %+v err %v", unrev, err)
	}
	read.Reviewed = true
	if err := s.UpdateEquipment(ctx, read); err != nil {
		t.Fatal(err)
	}
	unrev, err = s.ListUnreviewedEquipment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(unrev) != 0 {
		t.Errorf("still unreviewed: %+v", unrev)
	}
	if all, err := s.ListEquipment(ctx); err != nil || len(all) == 0 {
		t.Fatalf("ListEquipment: %d, %v", len(all), err)
	}
	if _, err := s.GetEquipment(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: %v", err)
	}
	if err := s.UpdateEquipment(ctx, Equipment{ID: 999999, Name: "zz test q"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update missing: %v", err)
	}
	if _, err := s.CreateEquipment(ctx, Equipment{Name: "skillet"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := s.CreateEquipment(ctx, Equipment{Name: ""}); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty: %v", err)
	}
}

func TestListsCarryAliases(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustIngredient(t, s, Ingredient{Name: "zz test list", Aisle: "a", Aliases: []string{"zz test zebra", "zz test apple"}})
	ings, err := s.ListUnreviewedIngredients(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ings) != 1 || len(ings[0].Aliases) != 2 || ings[0].Aliases[0] != "zz test apple" {
		t.Errorf("ListUnreviewedIngredients = %+v, want both aliases, sorted", ings)
	}
	units, err := s.ListUnits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range units {
		if u.Unit.Name == "tbsp" && len(u.Aliases) < 2 {
			t.Errorf("tbsp listed with aliases %v", u.Aliases)
		}
	}
	eq, err := s.ListEquipment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range eq {
		if e.Name == "skillet" && len(e.Aliases) == 0 {
			t.Error("skillet listed without aliases")
		}
	}
}

func TestUpdateConflictsWithAnotherEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	t.Run("ingredient", func(t *testing.T) {
		a := mustIngredient(t, s, Ingredient{Name: "zz test ua", Aisle: "a", Aliases: []string{"zz test ua alias"}})
		b := mustIngredient(t, s, Ingredient{Name: "zz test ub", Aisle: "a"})
		for _, bad := range []Ingredient{
			{ID: b.ID, Name: a.Name, Aisle: "a"},                                        // rename to another's name
			{ID: b.ID, Name: b.Name, Aisle: "a", Aliases: []string{a.Name}},             // alias equal to another's name
			{ID: b.ID, Name: b.Name, Aisle: "a", Aliases: []string{"zz test ua alias"}}, // alias equal to another's alias
			{ID: b.ID, Name: "zz test ua alias", Aisle: "a"},                            // name equal to another's alias
		} {
			if err := s.UpdateIngredient(ctx, bad); !errors.Is(err, ErrConflict) {
				t.Errorf("UpdateIngredient(%+v) = %v, want ErrConflict", bad, err)
			}
		}
		if got, err := s.GetIngredient(ctx, b.ID); err != nil || got.Name != b.Name || len(got.Aliases) != 0 {
			t.Errorf("failed updates changed the ingredient: %+v err %v", got, err)
		}
	})

	t.Run("unit", func(t *testing.T) {
		a, err := s.CreateUnit(ctx, Unit{Unit: quantity.OtherUnit("zz test ua"), Aliases: []string{"zz test ua alias"}})
		if err != nil {
			t.Fatal(err)
		}
		b, err := s.CreateUnit(ctx, Unit{Unit: quantity.OtherUnit("zz test ub")})
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []Unit{
			{ID: b.ID, Unit: quantity.OtherUnit(a.Unit.Name)},
			{ID: b.ID, Unit: quantity.OtherUnit(b.Unit.Name), Aliases: []string{a.Unit.Name}},
			{ID: b.ID, Unit: quantity.OtherUnit(b.Unit.Name), Aliases: []string{"zz test ua alias"}},
			{ID: b.ID, Unit: quantity.OtherUnit("zz test ua alias")},
		} {
			if err := s.UpdateUnit(ctx, bad); !errors.Is(err, ErrConflict) {
				t.Errorf("UpdateUnit(%+v) = %v, want ErrConflict", bad.Unit, err)
			}
		}
	})

	t.Run("equipment", func(t *testing.T) {
		a, err := s.CreateEquipment(ctx, Equipment{Name: "zz test ua", Aliases: []string{"zz test ua alias"}})
		if err != nil {
			t.Fatal(err)
		}
		b, err := s.CreateEquipment(ctx, Equipment{Name: "zz test ub"})
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []Equipment{
			{ID: b.ID, Name: a.Name},
			{ID: b.ID, Name: b.Name, Aliases: []string{a.Name}},
			{ID: b.ID, Name: b.Name, Aliases: []string{"zz test ua alias"}},
			{ID: b.ID, Name: "zz test ua alias"},
		} {
			if err := s.UpdateEquipment(ctx, bad); !errors.Is(err, ErrConflict) {
				t.Errorf("UpdateEquipment(%+v) = %v, want ErrConflict", bad, err)
			}
		}
	})
}

func TestWrapWriteOnlyMapsUniqueViolationsToConflict(t *testing.T) {
	d := newTestDB(t)
	mustIngredient(t, New(d), Ingredient{Name: "zz test dup", Aisle: "a"})

	cases := []struct {
		name         string
		query        string
		wantConflict bool
	}{
		{"unique name", `INSERT INTO ingredients (name, aisle) VALUES ('zz test dup', 'a')`, true},
		{"primary key alias", `INSERT INTO ingredient_aliases (alias, ingredient_id) SELECT 'zz test pk', id FROM ingredients WHERE name = 'zz test dup' UNION ALL SELECT 'zz test pk', id FROM ingredients WHERE name = 'zz test dup'`, true},
		{"check", `INSERT INTO ingredients (name, aisle, density_g_per_ml) VALUES ('zz test chk', 'a', -1)`, false},
		{"not null", `INSERT INTO ingredients (name, aisle) VALUES ('zz test nn', NULL)`, false},
		{"foreign key", `INSERT INTO ingredient_aliases (alias, ingredient_id) VALUES ('zz test fk', 999999)`, false},
	}
	for _, c := range cases {
		_, err := d.Exec(c.query)
		if err == nil {
			t.Fatalf("%s: statement unexpectedly succeeded", c.name)
		}
		if got := errors.Is(wrapWrite("test", err), ErrConflict); got != c.wantConflict {
			t.Errorf("%s: errors.Is(ErrConflict) = %v, want %v (err: %v)", c.name, got, c.wantConflict, err)
		}
	}
}
