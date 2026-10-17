package shopping

import (
	"errors"
	"reflect"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func TestKeyOf(t *testing.T) {
	tests := []struct {
		name string
		item Item
		want ItemKey
	}{
		{"resolved ingredient", Item{Ingredient: 7, Name: "Butter"}, "i:7"},
		{"resolved ignores amounts and sources", Item{
			Ingredient: 7, Name: "Butter", Amounts: []quantity.Amount{{}}, Sources: []Source{{RecipeID: 1}}, Manual: true,
		}, "i:7"},
		{"manual free text", Item{Name: "  Paper Towels "}, "t:paper towels"},
		{"unresolved line", Item{Name: "A Pinch of Mace", Unresolved: true, Sources: []Source{{RecipeID: 4, Name: "Stew"}}}, "u:4:a pinch of mace"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := KeyOf(tc.item); got != tc.want {
				t.Errorf("KeyOf = %q, want %q", got, tc.want)
			}
		})
	}
}

// The reason a key exists: it must not change when the amount does.
func TestKeyOfIsStableWhenAmountsChange(t *testing.T) {
	keyAt := func(mult int64) ItemKey {
		l := gen(t, Input{Entries: []RecipeEntry{entry(t, 1, "Cake", mult, line(idFlour, amt(t, 1, 1, "cup")))}})
		return KeyOf(l.Aisles[0].Items[0])
	}
	if a, b := keyAt(1), keyAt(3); a != b {
		t.Errorf("key changed with the amount: %q then %q", a, b)
	}
}

func TestItemKeyValid(t *testing.T) {
	for k, want := range map[ItemKey]bool{
		"i:1": true, "t:milk": true, "u:2:x": true,
		"": false, "i:": false, "x:1": false, "milk": false,
	} {
		if got := k.valid(); got != want {
			t.Errorf("%q.valid() = %v, want %v", k, got, want)
		}
	}
}

func TestAnnotateAndStaleKeys(t *testing.T) {
	l := gen(t, Input{
		Entries: []RecipeEntry{entry(t, 1, "Cake", 1, line(idFlour, amt(t, 1, 1, "cup")), line(idButter, amt(t, 1, 1, "tbsp")), line(idSalt, amt(t, 1, 1, "tsp")))},
		Pantry:  map[recipe.IngredientID]bool{idSalt: true},
	})
	stored := []ItemKey{ingKey(int64(idFlour)), "i:99", ingKey(int64(idSalt)), "t:gone"}

	checked := map[ItemKey]bool{}
	for _, k := range stored {
		checked[k] = true
	}
	got := map[string]bool{}
	for _, a := range annotate(l, checked) {
		for _, it := range a.Items {
			got[it.Name] = it.Checked
		}
	}
	if want := map[string]bool{"flour": true, "butter": false}; !reflect.DeepEqual(got, want) {
		t.Errorf("checked = %v, want %v", got, want)
	}

	// Keys naming no listed item go, including the pantry item set aside.
	want := []ItemKey{"i:99", ingKey(int64(idSalt)), "t:gone"}
	if stale := staleKeys(l, stored); !reflect.DeepEqual(stale, want) {
		t.Errorf("staleKeys = %v, want %v", stale, want)
	}
}

func TestCheckMultiplier(t *testing.T) {
	if err := checkMultiplier(quantity.Int(2)); err != nil {
		t.Errorf("2: %v", err)
	}
	for _, m := range []quantity.Rat{quantity.Int(0), quantity.Int(-1)} {
		if err := checkMultiplier(m); !errors.Is(err, ErrInvalid) || !errors.Is(err, ErrInvalidMultiplier) {
			t.Errorf("%s: err = %v, want ErrInvalid wrapping ErrInvalidMultiplier", m, err)
		}
	}
}

func TestViewByRecipeGroupsItemsUnderTheirRecipesWithTheirShares(t *testing.T) {
	l := gen(t, Input{
		Entries: []RecipeEntry{
			entry(t, 2, "Zebra cake", 1, line(idButter, amt(t, 1, 1, "tbsp")), line(idSalt, amt(t, 1, 1, "tsp"))),
			entry(t, 1, "Apple pie", 1, line(idButter, amt(t, 2, 1, "tbsp"))),
		},
		Manual: []ManualItem{{Ingredient: idButter, Amount: amt(t, 3, 1, "tbsp")}, {Text: "Paper towels"}},
	})
	v := View{
		Entries: []EntryView{{RecipeID: 2, Name: "Zebra cake"}, {RecipeID: 1, Name: "Apple pie"}},
		Aisles:  annotate(l, map[ItemKey]bool{"i:" + "1": true}),
	}
	groups := v.ByRecipe()

	var names []string
	for _, g := range groups {
		if g.Manual {
			names = append(names, "(by hand)")
		} else {
			names = append(names, g.Source.Name)
		}
	}
	if want := []string{"Zebra cake", "Apple pie", "(by hand)"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("groups = %q, want entry order then the manual group %q", names, want)
	}
	butter := func(g RecipeGroup) ShareItem {
		for _, it := range g.Items {
			if it.Name == "butter" {
				return it
			}
		}
		t.Fatalf("group %q has no butter", g.Source.Name)
		return ShareItem{}
	}
	if got := butter(groups[0]).Share.Amounts[0].String(); got != "1 tbsp" {
		t.Errorf("Zebra cake butter share = %q, want 1 tbsp", got)
	}
	if got := butter(groups[1]).Share.Amounts[0].String(); got != "2 tbsp" {
		t.Errorf("Apple pie butter share = %q, want 2 tbsp", got)
	}
	m := butter(groups[2])
	if got := m.Share.Amounts[0].String(); got != "3 tbsp" || !m.Checked || m.Key != "i:1" {
		t.Errorf("manual butter = %+v, want share 3 tbsp, the item's check and key", m)
	}
	if n := len(groups[2].Items); n != 2 {
		t.Errorf("manual group has %d items, want butter and paper towels", n)
	}
}

func TestViewByRecipeOmitsARecipeWithNothingOnTheList(t *testing.T) {
	l := gen(t, Input{
		Entries: []RecipeEntry{entry(t, 1, "Soup", 1, line(idSalt, amt(t, 1, 1, "tsp")))},
		Pantry:  map[recipe.IngredientID]bool{idSalt: true},
	})
	v := View{Entries: []EntryView{{RecipeID: 1, Name: "Soup"}}, Aisles: annotate(l, nil)}
	if g := v.ByRecipe(); len(g) != 0 {
		t.Errorf("groups = %+v, want none", g)
	}
}
