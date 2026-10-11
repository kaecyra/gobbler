package shopping

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// The golden text is inline: testdata/ is outside this node's file lane.
const goldenText = `Baking
- flour: 220 g

Dairy
- butter: 10 tbsp
- egg: 14

Produce
- garlic: 2 cup + 3 clove

Uncategorised
- foil
- mystery: 1
`

func TestTextGolden(t *testing.T) {
	l := gen(t, Input{
		Entries: []RecipeEntry{
			entry(t, 1, "A", 1,
				line(idButter, amt(t, 2, 1, "tbsp")),
				line(idButter, amt(t, 1, 2, "cup")),
				line(idFlour, amt(t, 1, 1, "cup")),
				line(idFlour, amt(t, 100, 1, "g")),
				line(idEgg, amt(t, 2, 1, "")),
				line(idEgg, amt(t, 1, 1, "dozen")),
				line(idGarlic, amt(t, 2, 1, "cup")),
				line(idGarlic, amt(t, 3, 1, "clove")),
				line(idMystery, amt(t, 1, 1, "")),
				line(idSalt, amt(t, 1, 1, "tsp")),
			),
		},
		Manual: []ManualItem{{Text: "foil"}},
		Pantry: map[recipe.IngredientID]bool{idSalt: true},
	})
	got, err := l.Text()
	if err != nil {
		t.Fatal(err)
	}
	if got != goldenText {
		t.Errorf("got:\n%s\nwant:\n%s", got, goldenText)
	}
}

func TestTextItemWithoutAmountsIsNameOnly(t *testing.T) {
	taste := line(idSalt, quantity.Amount{})
	taste.Rule = quantity.RuleToTaste
	l := gen(t, Input{Entries: []RecipeEntry{entry(t, 1, "A", 1, taste)}})
	got, err := l.Text()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Spices\n- salt\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTextEmptyList(t *testing.T) {
	got, err := List{}.Text()
	if err != nil || got != "" {
		t.Errorf("got %q, %v", got, err)
	}
}
