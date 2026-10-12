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
