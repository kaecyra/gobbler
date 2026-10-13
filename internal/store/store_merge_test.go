package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// overlayCatalog is seedCatalog with the four ingredients created in the
// overlay of the household authedCtx names, which is where a merge can reach
// them: a household never merges a central entry away (ADR-00018).
func overlayCatalog(t *testing.T, d *sql.DB) (*catalog.Store, catalogIDs) {
	t.Helper()
	c, ids := seedCatalog(t, d)
	ctx := authedCtx(t)
	ing := func(name string) int64 {
		in, err := c.CreateIngredient(ctx, catalog.Ingredient{Name: name, Aisle: "test"})
		if err != nil {
			t.Fatalf("CreateIngredient(%q): %v", name, err)
		}
		return in.ID
	}
	ids.flour, ids.butter, ids.egg, ids.salt = ing("zz overlay flour"), ing("zz overlay butter"), ing("zz overlay egg"), ing("zz overlay salt")
	return c, ids
}

func TestMergeWithoutHooksFailsOnTheForeignKey(t *testing.T) {
	s, d := newTestStore(t)
	c, ids := overlayCatalog(t, d)
	ctx := authedCtx(t)
	if _, err := s.Create(ctx, fullRecipe(t, ids)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MergeIngredients(ctx, ids.butter, ids.flour); err == nil {
		t.Fatal("merge succeeded although recipe lines reference the loser and no hook is registered")
	}
}

func TestMergeRepointsLinesAndRevisions(t *testing.T) {
	s, d := newTestStore(t)
	c, ids := overlayCatalog(t, d)
	s.RegisterMergeHooks(c)
	ctx := authedCtx(t)

	// Revisions 1 and 2 name flour (revision 1 also names the winner, butter,
	// so the merge must not leave two index rows); the live document, version
	// 3, names neither.
	v1 := fullRecipe(t, ids)
	id, err := s.Create(ctx, v1)
	if err != nil {
		t.Fatal(err)
	}
	v2 := fullRecipe(t, ids)
	v2.Components[0].Lines = v2.Components[0].Lines[:1] // flour only
	if err := s.Update(ctx, id, v2); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, id, simpleRecipe("v3")); err != nil {
		t.Fatal(err)
	}

	if _, err := c.MergeIngredients(ctx, ids.butter, ids.flour); err != nil {
		t.Fatalf("MergeIngredients: %v", err)
	}

	// Live lines point at the winner; nothing points at the loser.
	if n := count(t, d, `SELECT COUNT(*) FROM ingredient_lines WHERE ingredient_id = ?`, ids.flour); n != 0 {
		t.Errorf("%d lines still reference the merged ingredient", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revision_ingredients WHERE ingredient_id = ?`, ids.flour); n != 0 {
		t.Errorf("%d revision index rows still reference the merged ingredient", n)
	}

	// Revision documents were rewritten, so restoring an old version works and
	// uses the winner.
	if err := s.Restore(ctx, id, 1); err != nil {
		t.Fatalf("Restore after merge: %v", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := fullRecipe(t, ids)
	want.Components[0].Lines[0].Ingredient = recipe.IngredientID(ids.butter)
	sameRecipe(t, want, got)

	// Revision 1 names butter twice over (flour became butter): one index row.
	var rev1 int64
	if err := d.QueryRow(`SELECT id FROM revisions WHERE version = 1`).Scan(&rev1); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM revision_ingredients WHERE revision_id = ? AND ingredient_id = ?`, rev1, ids.butter); n != 1 {
		t.Errorf("index rows for the winner in revision 1 = %d, want 1", n)
	}
}

func TestMergeRollsBackWithHooks(t *testing.T) {
	s, d := newTestStore(t)
	c, ids := overlayCatalog(t, d)
	s.RegisterMergeHooks(c)
	boom := errors.New("later hook fails")
	c.OnIngredientMerge(func(context.Context, *sql.Tx, int64, int64) error { return boom })
	ctx := authedCtx(t)
	id, err := s.Create(ctx, fullRecipe(t, ids))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.MergeIngredients(ctx, ids.butter, ids.flour); !errors.Is(err, boom) {
		t.Fatalf("MergeIngredients = %v, want the hook's error", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sameRecipe(t, fullRecipe(t, ids), got) // still flour
}

func TestMergeRepointsLiveLines(t *testing.T) {
	s, d := newTestStore(t)
	c, ids := overlayCatalog(t, d)
	s.RegisterMergeHooks(c)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, fullRecipe(t, ids))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.MergeIngredients(ctx, ids.butter, ids.flour); err != nil {
		t.Fatalf("MergeIngredients: %v", err)
	}
	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := fullRecipe(t, ids)
	want.Components[0].Lines[0].Ingredient = recipe.IngredientID(ids.butter)
	sameRecipe(t, want, got)
}
