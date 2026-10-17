package ingest

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// A stage kind is proposed only where the text is unambiguous: precision over
// recall, since an unproposed kind is only empty (ADR-00006, low confidence is
// data) while a wrong one mislabels the time ribbon.
func TestStageKindIsProposedFromTheInstruction(t *testing.T) {
	cases := []struct {
		text string
		want recipe.StageKind
	}{
		{"Bake for 30 minutes.", recipe.StageBake},
		{"Preheat the oven. Bake until golden.", recipe.StageBake},
		{"Roast the potatoes for 40 minutes.", recipe.StageRoast},
		{"Simmer gently for 20 minutes.", recipe.StageSimmer},
		{"Braise the short ribs for 3 hours.", recipe.StageBraise},
		{"Marinate the chicken overnight.", recipe.StageMarinate},
		{"Chill for 2 hours.", recipe.StageChill},
		{"Refrigerate overnight.", recipe.StageChill},
		{"Steep the tea for 4 minutes.", recipe.StageSteep},
		{"Let the dough rise for 1 hour.", recipe.StageRise},
		{"Proof the dough until doubled.", recipe.StageRise},
		{"Let the steak rest for 10 minutes.", recipe.StageRest},
		{"Rest for 5 minutes.", recipe.StageRest},
		{"Saute the onions until soft.", recipe.StageCook},
		{"Sauté the onions until soft.", recipe.StageCook},
		{"Sautéed onions are folded in.", ""},
		{"Cook the pasta until al dente.", recipe.StageCook},
		{"Chop the onions finely.", recipe.StagePrep},
		{"Peel and dice the carrots.", recipe.StagePrep},

		// Nothing the vocabulary reads.
		{"Mix the flour and water.", ""},
		{"Serve with rice.", ""},
		{"", ""},
		// Not instructions: adjectives, ingredients, other phrases.
		{"Add the roasted peppers and baked beans.", ""},
		{"Add the carrots and roast beef.", ""},
		{"Use a stand mixer on low.", ""},
		{"Stir while the sauce simmers.", ""},
		// Two kinds in one step is ambiguous, so none is proposed.
		{"Chop the onion and cook until soft.", ""},
		{"Let the dough rise, then bake for 20 minutes.", ""},
		{"Cook the rice, then simmer for 10 minutes.", ""},
	}
	for _, c := range cases {
		if got := stageKind(c.text); got != c.want {
			t.Errorf("stageKind(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestRuleDraftProposesAStageKindPerStep(t *testing.T) {
	e := newEnv(t)
	got := flatSteps(ruleDraft(t, e, recipeWith("Mix the flour and water.", "Let the dough rise for 1 hour.", "Bake for 30 minutes.")))
	want := []recipe.StageKind{"", recipe.StageRise, recipe.StageBake}
	if len(got) != len(want) {
		t.Fatalf("steps = %+v", got)
	}
	for i, st := range got {
		if st.Kind != want[i] {
			t.Errorf("step %d kind = %q, want %q", i+1, st.Kind, want[i])
		}
	}
}

func TestSavePersistsStageKindTitleMealTypeAndSpice(t *testing.T) {
	e := newEnv(t)
	d := ruleDraft(t, e, recipeWith("Mix the flour and water.", "Bake for 30 minutes."))
	d.MealType = recipe.MealDessert
	spice := 2
	d.SpiceLevel = &spice
	d.Components[0].Steps[0].Title = "Make the dough"
	saved, err := e.saver().Save(e.ctx, confirm(t, d))
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.store.Get(e.ctx, saved.RecipeID)
	if err != nil {
		t.Fatal(err)
	}
	if got.MealType != recipe.MealDessert || got.SpiceLevel == nil || *got.SpiceLevel != 2 {
		t.Errorf("meal type %q spice %v, want dessert and 2", got.MealType, got.SpiceLevel)
	}
	steps := got.Components[0].Steps
	if steps[0].Title != "Make the dough" || steps[0].Kind != "" || steps[1].Kind != recipe.StageBake {
		t.Errorf("steps = %+v, want title on the first and bake on the second", steps)
	}
}
