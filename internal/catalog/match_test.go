package catalog

import (
	"context"
	"slices"
	"testing"
)

func TestInflections(t *testing.T) {
	cases := []struct {
		in   string
		want []string // must appear, in this relative order after the term itself
	}{
		{"tomatoes", []string{"tomatoes", "tomato"}},
		{"strawberries", []string{"strawberries", "strawberry"}},
		{"leaves", []string{"leaves", "leaf"}},
		{"knives", []string{"knives", "knife"}},
		{"cherry tomatoes", []string{"cherry tomatoes", "cherry tomato"}},
		{"egg", []string{"egg", "eggs"}},
		{"berry", []string{"berry", "berries"}},
		{"glass", []string{"glass", "glasses"}},
	}
	for _, c := range cases {
		got := inflections(c.in)
		if got[0] != c.in {
			t.Errorf("inflections(%q)[0] = %q, want the term itself", c.in, got[0])
		}
		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("inflections(%q) = %v, missing %q", c.in, got, w)
			}
		}
	}
	if got := inflections("glass"); slices.Contains(got, "glas") {
		t.Errorf("inflections(glass) = %v must not strip the s of a double-s word", got)
	}
	if got := inflections(""); got != nil {
		t.Errorf("inflections(empty) = %v, want nil", got)
	}
}

func TestMatchIngredient(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mk := func(name, aisle string, aliases ...string) Ingredient {
		t.Helper()
		got, err := s.CreateIngredient(ctx, Ingredient{Name: name, Aisle: aisle, Aliases: aliases, Reviewed: true})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	chive := mk("zz test chive", "produce", "zz test chivelet")
	berry := mk("zz test berry", "produce")

	cases := []struct {
		term string
		want int64
		ok   bool
	}{
		{"zz test chive", chive.ID, true},
		{"  ZZ Test   CHIVE ", chive.ID, true}, // case and whitespace
		{"zz test chivelet", chive.ID, true},   // alias
		{"zz test chivelets", chive.ID, true},  // plural of alias
		{"zz test chives", chive.ID, true},     // plural of name
		{"zz test berries", berry.ID, true},    // y -> ies
		{"zz test nothing", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok, err := s.MatchIngredient(ctx, c.term)
		if err != nil {
			t.Fatalf("MatchIngredient(%q): %v", c.term, err)
		}
		if ok != c.ok || (ok && got.ID != c.want) {
			t.Errorf("MatchIngredient(%q) = (%d, %v), want (%d, %v)", c.term, got.ID, ok, c.want, c.ok)
		}
	}
}

func TestMatchPrefersExactOverInflected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := mustIngredient(t, s, Ingredient{Name: "zz test oat", Aisle: "baking"})
	b := mustIngredient(t, s, Ingredient{Name: "zz test oats", Aisle: "baking"})
	for term, want := range map[string]int64{"zz test oat": a.ID, "zz test oats": b.ID} {
		got, ok, err := s.MatchIngredient(ctx, term)
		if err != nil || !ok || got.ID != want {
			t.Errorf("MatchIngredient(%q) = (%d, %v, %v), want id %d", term, got.ID, ok, err, want)
		}
	}
}

func TestMatchNeverCreates(t *testing.T) {
	d := newTestDB(t)
	s := New(d)
	before := count(t, d, `SELECT count(*) FROM ingredients`)
	beforeAliases := count(t, d, `SELECT count(*) FROM ingredient_aliases`)
	for _, term := range []string{"dragonfruit", "dragonfruits", "unobtainium"} {
		if _, ok, err := s.MatchIngredient(context.Background(), term); err != nil || ok {
			t.Fatalf("MatchIngredient(%q) = ok %v err %v, want clean not-found", term, ok, err)
		}
	}
	if got := count(t, d, `SELECT count(*) FROM ingredients`); got != before {
		t.Errorf("ingredient rows changed from %d to %d by matching", before, got)
	}
	if got := count(t, d, `SELECT count(*) FROM ingredient_aliases`); got != beforeAliases {
		t.Errorf("alias rows changed from %d to %d by matching", beforeAliases, got)
	}
}

func TestMatchUnitAndEquipment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u, ok, err := s.MatchUnit(ctx, "Tablespoons")
	if err != nil || !ok || u.Unit.Name != "tbsp" {
		t.Errorf("MatchUnit(Tablespoons) = %+v ok=%v err=%v, want tbsp", u.Unit, ok, err)
	}
	if _, ok, err := s.MatchUnit(ctx, "furlong"); err != nil || ok {
		t.Errorf("MatchUnit(furlong) ok=%v err=%v, want clean not-found", ok, err)
	}
	e, ok, err := s.MatchEquipment(ctx, "Frying Pans")
	if err != nil || !ok || e.Name != "skillet" {
		t.Errorf("MatchEquipment(Frying Pans) = %+v ok=%v err=%v, want skillet", e, ok, err)
	}
	if _, ok, err := s.MatchEquipment(ctx, "trebuchet"); err != nil || ok {
		t.Errorf("MatchEquipment(trebuchet) ok=%v err=%v, want clean not-found", ok, err)
	}
}
