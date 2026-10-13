package store

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/catalog"
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
