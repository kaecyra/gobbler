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
		{"Serve with rice.", recipe.StageServe},
		{"Serving suggestion: with rice.", recipe.StageServe},

		// Nothing the vocabulary reads.
		{"Mix the flour and water.", ""},
		{"", ""},
		// Not instructions: adjectives, ingredients, other phrases.
		{"Add the roasted peppers and baked beans.", ""},
		{"Add the carrots and roast beef.", ""},
		{"Stir while the sauce simmers.", ""},
		{"Stir before you serve.", ""},
		{"Observe the dough.", ""},
		// Serving is a kind only when the text opens with it; later, the verbs decide.
		{"Slice and serve.", recipe.StagePrep},
		{"Chill for 2 hours, then serve.", recipe.StageChill},
		// An opening "serve" beside another kind is two kinds.
		{"Serve. Chop the parsley and cook the rice.", ""},
		{"Serve immediately. Refrigerate leftovers for up to 3 days.", ""},
		{"Serve and slice at the table.", ""},
		// Two kinds in one step is ambiguous, so none is proposed.
		{"Chop the onion and cook until soft.", ""},
		{"Let the dough rise, then bake for 20 minutes.", ""},
		{"Cook the rice, then simmer for 10 minutes.", ""},
	}
	for _, c := range cases {
		if got := stageKindOf("", c.text); got != c.want {
			t.Errorf("stageKindOf(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// mix is machine mixing and kneading (ADR-00022). Mixing, stirring, whisking
// or combining by hand is not, and a step that names mix beside another kind
// is ambiguous and gets none (ADR-00006, precision over recall).
func TestStageKindMixIsMachineMixingAndKneading(t *testing.T) {
	cases := []struct {
		text string
		want recipe.StageKind
	}{
		{"Knead the dough for 10 minutes until smooth.", recipe.StageMix},
		{"Turn the dough out and knead it until elastic.", recipe.StageMix},
		{"Continue kneading until the dough passes the windowpane test.", recipe.StageMix},
		{"Use a stand mixer on low.", recipe.StageMix},
		{"In the bowl of a stand mixer, combine the flour, yeast and water.", recipe.StageMix},
		{"Beat the butter and sugar with a hand mixer until fluffy.", recipe.StageMix},
		{"Beat the cream with an electric mixer to soft peaks.", recipe.StageMix},
		{"Pulse the oats in a food processor until fine.", recipe.StageMix},
		{"Pour the soup into a blender and puree.", recipe.StageMix},
		{"Add everything to the bread machine and start the dough cycle.", recipe.StageMix},
		{"Mix on medium speed with the dough hook for 5 minutes.", recipe.StageMix},

		// Mixing by hand is not mix.
		{"Mix the flour and water.", ""},
		{"Stir in the chocolate chips.", ""},
		{"Whisk the eggs and milk together.", ""},
		{"Combine the dry ingredients in a large bowl.", ""},
		{"Mix well, by hand.", ""},
		// Not the instruction: an adjective, a kind of bread, a noun.
		{"Shape the kneaded dough into a ball.", ""},
		{"This no-knead dough needs no work.", ""},
		{"Sprinkle with the spice blend.", ""},
		// Blending by hand is not mix; only a blender is.
		{"Blend until smooth.", ""},
		{"Using a pastry cutter, blend the butter into the flour.", ""},
		{"Blend in the butter with your fingertips.", ""},
		{"Stir to blend.", ""},
		{"Blend the spices together in a small bowl.", ""},
		{"Top with your mixer of choice.", ""},
		// mix beside another kind is two kinds.
		{"Knead the dough, then let it rise for 1 hour.", ""},
		{"Chop the nuts in a food processor.", ""},
		{"Blend the soup in a blender, then simmer for 10 minutes.", ""},
	}
	for _, c := range cases {
		if got := stageKindOf("", c.text); got != c.want {
			t.Errorf("stageKindOf(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// A source title whose only kind is mix says so, whatever the text names.
func TestStageKindMixFromTheTitle(t *testing.T) {
	cases := []struct {
		title, text string
		want        recipe.StageKind
	}{
		{"Knead the dough", "Work it for 10 minutes until smooth.", recipe.StageMix},
		{"Knead", "Knead for 8 minutes, then rest for 5 minutes.", recipe.StageMix},
		{"In the stand mixer", "Beat the eggs and sugar until pale.", recipe.StageMix},
		{"Mix the dough", "Stir the flour and water together.", ""},
		{"Knead and let rise", "Work the dough, then leave it.", ""},
		{"Serve", "Knead the dough.", recipe.StageServe},
	}
	for _, c := range cases {
		if got := stageKindOf(c.title, c.text); got != c.want {
			t.Errorf("stageKindOf(%q, %q) = %q, want %q", c.title, c.text, got, c.want)
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
