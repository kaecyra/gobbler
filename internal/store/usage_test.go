package store

import (
	"context"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func TestUsageCountsRecipeLinesAndMatchesWhatAMergeRepoints(t *testing.T) {
	s, d := newTestStore(t)
	c, ids := overlayCatalog(t, d)
	s.RegisterMergeHooks(c)
	u := catalog.NewUsage(c)
	s.RegisterUsageCounters(u)
	ctx := authedCtx(t)

	flour := func() int64 {
		n, err := u.IngredientUsage(ctx, ids.flour)
		if err != nil {
			t.Fatalf("IngredientUsage(flour): %v", err)
		}
		return n
	}
	if n := flour(); n != 0 {
		t.Fatalf("unused ingredient counts %d lines, want 0", n)
	}
	for _, name := range []string{"Pancakes", "Waffles"} {
		r := fullRecipe(t, ids)
		r.Name = name
		if _, err := s.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	want := int64(count(t, d, `SELECT COUNT(*) FROM ingredient_lines WHERE ingredient_id = ?`, ids.flour))
	if want == 0 {
		t.Fatal("test recipe has no flour line")
	}
	if got := flour(); got != want {
		t.Fatalf("usage = %d, want %d lines", got, want)
	}

	if _, err := c.MergeIngredients(ctx, ids.butter, ids.flour); err != nil {
		t.Fatal(err)
	}
	if got, err := u.IngredientUsage(ctx, ids.butter); err != nil || got < want {
		t.Fatalf("after merge the winner counts %d, %v; want at least the %d re-pointed lines", got, err, want)
	}
}

func TestRecipeCountsByIngredientCountsTheHouseholdsRecipesOnce(t *testing.T) {
	s, d := newTestStore(t)
	_, ids := seedCatalog(t, d)
	ctx := authedCtx(t)

	counts := func(ctx context.Context) map[int64]int64 {
		t.Helper()
		got, err := s.RecipeCountsByIngredient(ctx)
		if err != nil {
			t.Fatalf("RecipeCountsByIngredient: %v", err)
		}
		return got
	}
	if got := counts(ctx); len(got) != 0 {
		t.Fatalf("no recipes yet, counts = %v; want none", got)
	}

	// Flour twice in one recipe still counts that recipe once.
	r := fullRecipe(t, ids)
	r.Components[1].Lines = append(r.Components[1].Lines, recipe.IngredientLine{
		Amount: quantity.Amount{Qty: qty(t, "1"), Unit: quantity.MustUnit("tbsp")}, Ingredient: recipe.IngredientID(ids.flour),
		Rule: quantity.RuleLinear, Raw: "1 tbsp flour",
	})
	if _, err := s.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	second := simpleRecipe("Bread")
	second.Components[0].Lines[0] = recipe.IngredientLine{Ingredient: recipe.IngredientID(ids.flour), Rule: quantity.RuleLinear, Raw: "flour"}
	if _, err := s.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	// Another household's recipe names the same central flour.
	if _, err := s.Create(otherCtx(t), fullRecipe(t, ids)); err != nil {
		t.Fatal(err)
	}

	got := counts(ctx)
	if got[ids.flour] != 2 {
		t.Errorf("flour used in %d recipes, want 2 (this household's two, the duplicate line once, the other household's none)", got[ids.flour])
	}
	if got[ids.egg] != 1 {
		t.Errorf("egg used in %d recipes, want 1", got[ids.egg])
	}
	if _, ok := got[0]; ok {
		t.Error("unresolved lines are counted under ingredient 0")
	}

	if _, err := s.RecipeCountsByIngredient(noHouseholdCtx(t)); !errors.Is(err, ErrNoHousehold) {
		t.Errorf("no household: err = %v, want ErrNoHousehold", err)
	}
}
