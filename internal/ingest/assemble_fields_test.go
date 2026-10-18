package ingest

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Only a category that names one meal type maps; several that disagree, or one
// the vocabulary does not know, leave it empty for the user (ADR-00006).
func TestMealTypeFromRecipeCategory(t *testing.T) {
	cases := []struct {
		in   []string
		want recipe.MealType
	}{
		{[]string{"Breakfast"}, recipe.MealBreakfast},
		{[]string{"Brunch"}, recipe.MealBreakfast},
		{[]string{"Dessert"}, recipe.MealDessert},
		{[]string{"Main Course"}, recipe.MealMain},
		{[]string{"Dinner"}, recipe.MealMain},
		{[]string{"Entrée"}, recipe.MealMain},
		{[]string{"entree"}, recipe.MealMain},
		{[]string{"Side Dish"}, recipe.MealSide},
		{[]string{"Snack"}, recipe.MealSnack},
		{[]string{"Drink"}, recipe.MealDrink},
		{[]string{"Beverage"}, recipe.MealDrink},
		{[]string{"  main   course "}, recipe.MealMain},
		// One string may list several, comma separated.
		{[]string{"Dinner, Main Course"}, recipe.MealMain},
		{[]string{"Dinner", "Main Course"}, recipe.MealMain},
		// An unknown one beside a known one does not veto it.
		{[]string{"Italian", "Dinner"}, recipe.MealMain},
		// Different meal types, or nothing known: empty.
		{[]string{"Dessert", "Dinner"}, ""},
		{[]string{"Breakfast, Snack"}, ""},
		{[]string{"Italian"}, ""},
		{[]string{"Lunch"}, ""},
		{[]string{""}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := mealTypeFor(c.in); got != c.want {
			t.Errorf("mealTypeFor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A cuisine maps by name or by an explicit synonym; a value that names no one
// cuisine leaves it empty and never vetoes a later one (ADR-00025).
func TestCuisineFromRecipeCuisine(t *testing.T) {
	cases := []struct {
		in   []string
		want recipe.Cuisine
	}{
		{[]string{"Japanese"}, recipe.CuisineJapanese},
		{[]string{"  middle   EASTERN "}, recipe.CuisineMiddleEastern},
		{[]string{"Japanese food"}, recipe.CuisineJapanese},
		{[]string{"Tex Mex"}, recipe.CuisineMexican},
		{[]string{"Other"}, recipe.CuisineOther},
		{[]string{"Italian, French"}, recipe.CuisineItalian},
		{[]string{"Asian", "Thai"}, recipe.CuisineThai},
		{[]string{"Mexican", "Tex Mex"}, recipe.CuisineMexican},
		{[]string{"Asian"}, ""},
		{[]string{"Asian-inspired"}, ""},
		{[]string{""}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := cuisineFor(c.in); got != c.want {
			t.Errorf("cuisineFor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for syn, c := range cuisineSynonyms {
		if !c.Valid() || syn != normalizeName(syn) {
			t.Errorf("synonym %q -> %q is not a normalised key for a listed cuisine", syn, c)
		}
	}
}

func TestAssembleTakesMealTypeAndStepTitlesFromTheSource(t *testing.T) {
	e := newEnv(t)
	res := extract.Result{Method: extract.MethodJSONLD, Recipe: &extract.Raw{
		Name: "Rice", Categories: []string{"Dessert"}, Ingredients: []string{"1 cup rice"},
		Instructions: []extract.InstructionStep{{Title: " Cook ", Text: "Simmer the rice."}, {Text: "Serve."}},
	}}
	d, err := e.pipeline(t, nil).assemble(e.ctx, Source{}, res)
	if err != nil {
		t.Fatal(err)
	}
	steps := flatSteps(d)
	if d.MealType != recipe.MealDessert || d.SpiceLevel != nil || steps[0].Title != "Cook" || steps[1].Title != "" {
		t.Errorf("meal %q spice %v titles %q %q, want dessert, none, Cook and none", d.MealType, d.SpiceLevel, steps[0].Title, steps[1].Title)
	}
}

func TestParserVersionCoversStageKindsAndMealType(t *testing.T) {
	// A draft with a meal type, titles or stage kinds must be told apart from
	// one made before the rules read them (ParserVersion's contract).
	if ParserVersion == "6" {
		t.Errorf("ParserVersion %q predates meal type and stage kind proposals", ParserVersion)
	}
}
