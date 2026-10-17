package store

import (
	"context"
	"errors"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// withFields sets every ADR-00022 field on a simple recipe.
func withFields(name string, meal recipe.MealType, spice *int, kind recipe.StageKind, title string) recipe.Recipe {
	r := simpleRecipe(name)
	r.MealType, r.SpiceLevel = meal, spice
	r.Components[0].Steps[0].Kind, r.Components[0].Steps[0].Title = kind, title
	return r
}

type fieldsOf struct {
	meal  recipe.MealType
	spice *int
	kind  recipe.StageKind
	title string
}

func readFields(ctx context.Context, t *testing.T, s *Store, id int64) fieldsOf {
	t.Helper()
	r, err := s.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	st := r.Components[0].Steps[0]
	return fieldsOf{r.MealType, r.SpiceLevel, st.Kind, st.Title}
}

func checkFields(t *testing.T, when string, got, want fieldsOf) {
	t.Helper()
	same := got.meal == want.meal && got.kind == want.kind && got.title == want.title &&
		(got.spice == nil) == (want.spice == nil) && (got.spice == nil || *got.spice == *want.spice)
	if !same {
		t.Errorf("%s: meal %q spice %v kind %q title %q; want meal %q spice %v kind %q title %q",
			when, got.meal, spiceText(got.spice), got.kind, got.title, want.meal, spiceText(want.spice), want.kind, want.title)
	}
}

func spiceText(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// Obligation 2 of ADR-00022: meal type, spice level, stage kind and step title
// are part of the recipe revision, so they survive an edit, a restore and a
// shared copy.
func TestRecipeFieldsSurviveEditRestoreAndAShare(t *testing.T) {
	s, _ := newTestStore(t)
	a, b := authedCtx(t), otherCtx(t)
	first := fieldsOf{recipe.MealDessert, ptr(0), recipe.StageBake, "Bake it"}
	second := fieldsOf{recipe.MealMain, ptr(3), recipe.StageSimmer, "Simmer"}

	id, err := s.Create(a, withFields("Cake", first.meal, first.spice, first.kind, first.title))
	if err != nil {
		t.Fatal(err)
	}
	checkFields(t, "after create", readFields(a, t, s, id), first)

	if err := s.Update(a, id, withFields("Cake", second.meal, second.spice, second.kind, second.title)); err != nil {
		t.Fatal(err)
	}
	checkFields(t, "after an edit", readFields(a, t, s, id), second)
	old, err := s.GetVersion(a, id, 1)
	if err != nil || old.MealType != first.meal || old.Components[0].Steps[0].Kind != first.kind {
		t.Errorf("version 1 = meal %q kind %q (%v): a revision holds the fields it replaced", old.MealType, old.Components[0].Steps[0].Kind, err)
	}

	// Clearing them is an edit too; a zero spice level and an unset one differ.
	if err := s.Update(a, id, simpleRecipe("Cake")); err != nil {
		t.Fatal(err)
	}
	checkFields(t, "after clearing", readFields(a, t, s, id), fieldsOf{})

	if err := s.Restore(a, id, 2); err != nil {
		t.Fatal(err)
	}
	checkFields(t, "after restoring the version that had the second set", readFields(a, t, s, id), second)
	if err := s.Restore(a, id, 1); err != nil {
		t.Fatal(err)
	}
	checkFields(t, "after restoring the first version (spice 0, not unset)", readFields(a, t, s, id), first)

	cloneID, err := s.Clone(a, id)
	if err != nil {
		t.Fatal(err)
	}
	checkFields(t, "clone", readFields(a, t, s, cloneID), first)

	shareID, err := s.SendShare(a, id, otherHouseholdID)
	if err != nil {
		t.Fatal(err)
	}
	copyID, err := s.AcceptShare(b, shareID, fixed(nil))
	if err != nil {
		t.Fatal(err)
	}
	checkFields(t, "shared copy", readFields(b, t, s, copyID), first)
}

func TestInvalidRecipeFieldsWriteNothing(t *testing.T) {
	s, d := newTestStore(t)
	a := authedCtx(t)
	for name, tc := range map[string]struct {
		r    recipe.Recipe
		want error
	}{
		"meal type":  {withFields("A", "brunch", nil, "", ""), recipe.ErrInvalidMealType},
		"spice":      {withFields("B", "", ptr(4), "", ""), recipe.ErrInvalidSpiceLevel},
		"stage kind": {withFields("C", "", nil, "fry", ""), recipe.ErrInvalidStageKind},
	} {
		if _, err := s.Create(a, tc.r); !errors.Is(err, tc.want) {
			t.Errorf("Create with a bad %s = %v, want %v", name, err, tc.want)
		}
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != 0 {
		t.Errorf("rejected documents left %d recipes", n)
	}
}

// The columns carry their own CHECK constraints, so a value that bypasses the
// model is still refused.
func TestRecipeFieldColumnsRefuseValuesOutsideTheirSets(t *testing.T) {
	s, d := newTestStore(t)
	a := authedCtx(t)
	id, err := s.Create(a, withFields("Cake", "", nil, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddNote(a, id, "a tip"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`UPDATE recipes SET meal_type = 'brunch'`,
		`UPDATE recipes SET spice_level = 4`,
		`UPDATE recipes SET spice_level = -1`,
		`UPDATE steps SET stage_kind = 'fry'`,
		`UPDATE notes SET pinned = 2`,
	} {
		if _, err := d.Exec(q); err == nil {
			t.Errorf("%s was accepted", q)
		}
	}
}

// A revision document written before migration 00014 has none of the new keys.
// Reading it, and restoring it, gives the fields empty, not an error.
func TestRevisionDocumentFromBefore00014DecodesWithTheFieldsEmpty(t *testing.T) {
	s, d := newTestStore(t)
	a := authedCtx(t)
	id, err := s.Create(a, withFields("Cake", recipe.MealMain, ptr(3), recipe.StageBake, "Bake it"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(a, id, withFields("Cake", recipe.MealMain, ptr(3), recipe.StageBake, "Bake it")); err != nil {
		t.Fatal(err)
	}
	const old = `{"name":"Cake","components":[{"lines":[{"rule":"linear","raw":"water"}],"steps":[{"id":"s1","text":"Do it.","passive":true}]}]}`
	if _, err := d.Exec(`UPDATE revisions SET document = ? WHERE recipe_id = ? AND version = 1`, old, id); err != nil {
		t.Fatal(err)
	}

	r, err := s.GetVersion(a, id, 1)
	if err != nil {
		t.Fatalf("GetVersion of an old document: %v", err)
	}
	if r.MealType != "" || r.SpiceLevel != nil || r.Components[0].Steps[0].Kind != "" || r.Components[0].Steps[0].Title != "" {
		t.Errorf("old document decoded with meal %q spice %v step %+v, want all empty", r.MealType, r.SpiceLevel, r.Components[0].Steps[0])
	}
	if err := s.Restore(a, id, 1); err != nil {
		t.Fatal(err)
	}
	checkFields(t, "after restoring the old revision", readFields(a, t, s, id), fieldsOf{})
}
