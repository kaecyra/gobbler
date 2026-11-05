package editor

import (
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func sline(key string, ing int64, qty int64) recipe.IngredientLine {
	return recipe.IngredientLine{Key: key, Ingredient: recipe.IngredientID(ing), Rule: quantity.RuleLinear,
		Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(qty)), Unit: quantity.MustUnit("cup")}}
}

func TestSummarizeSaysWhatChangedInPlainSentences(t *testing.T) {
	names := lookups{ingredient: map[int64]string{1: "flour", 2: "salt"}, equipment: map[int64]string{}}
	cur := recipe.Recipe{Name: "Bread", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{sline("lin-aaaaaaaa", 1, 2), sline("lin-bbbbbbbb", 2, 1)},
		Steps: []recipe.Step{{ID: "stp-aaaaaaaa", Text: "Mix."}, {ID: "stp-bbbbbbbb", Text: "Bake."}},
	}}}
	next := recipe.Recipe{Name: "Better bread", Components: []recipe.Component{{
		Lines: []recipe.IngredientLine{sline("lin-aaaaaaaa", 1, 3), sline("", 2, 4)},
		Steps: []recipe.Step{{ID: "stp-aaaaaaaa", Text: "Mix well."}},
	}}}
	got := strings.Join(summarize(cur, next, names), "\n")
	for _, want := range []string{
		"Name: Bread, now Better bread",
		"Ingredient changed: 2 cup flour, now 3 cup flour",
		"Ingredient removed: 1 cup salt",
		"Ingredient added: 4 cup salt",
		"Step 1 changed: Mix., now Mix well.",
		"Step 2 removed: Bake.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
}

func TestSummarizeOfAnUnchangedRecipeIsEmpty(t *testing.T) {
	r := recipe.Recipe{Name: "Bread", Components: []recipe.Component{{Steps: []recipe.Step{{ID: "stp-aaaaaaaa", Text: "Mix."}}}}}
	if got := summarize(r, r, lookups{}); len(got) != 0 {
		t.Errorf("summary = %v, want none", got)
	}
}
