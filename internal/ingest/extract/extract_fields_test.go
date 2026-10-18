package extract

import (
	"reflect"
	"testing"
)

// recipeCategory is read as the page wrote it: a list stays a list. Mapping it
// to a meal type is the assembler's job.
func TestJSONLDReadsRecipeCategory(t *testing.T) {
	res := mustExtract(t, fixture(t, "jsonld_category"), fixtureURL)
	if want := []string{"Dessert", "Dinner"}; !reflect.DeepEqual(res.Recipe.Categories, want) {
		t.Errorf("categories = %v, want %v", res.Recipe.Categories, want)
	}
}

// recipeCuisine is read as the page wrote it, a string or a list (ADR-00025).
func TestJSONLDReadsRecipeCuisineAsStringOrList(t *testing.T) {
	for name, want := range map[string][]string{
		"real_recipetineats_graph": {"Japanese"},
		"real_wprm_graph":          {"Mexican", "Tex Mex"},
		"real_yoast_graph":         {"American"},
	} {
		res := mustExtract(t, fixture(t, name), fixtureURL)
		if !reflect.DeepEqual(res.Recipe.Cuisines, want) {
			t.Errorf("%s: cuisines = %v, want %v", name, res.Recipe.Cuisines, want)
		}
	}
}

func TestMicrodataReadsRecipeCuisine(t *testing.T) {
	res := mustExtract(t, fixture(t, "microdata_cuisine"), fixtureURL)
	if want := []string{"Thai"}; !reflect.DeepEqual(res.Recipe.Cuisines, want) {
		t.Errorf("cuisines = %v, want %v", res.Recipe.Cuisines, want)
	}
}

func TestMicrodataReadsRecipeCategory(t *testing.T) {
	res := mustExtract(t, fixture(t, "microdata_category"), fixtureURL)
	if want := []string{"Beverage"}; !reflect.DeepEqual(res.Recipe.Categories, want) {
		t.Errorf("categories = %v, want %v", res.Recipe.Categories, want)
	}
}

// A HowToStep name is the step's title only when it adds something: not when
// it repeats the text, is missing, stands in for missing text, or is a bare
// "Step 3".
func TestJSONLDStepTitleIsTheNameWhenItDiffersFromTheText(t *testing.T) {
	res := mustExtract(t, fixture(t, "jsonld_category"), fixtureURL)
	var got []string
	for _, s := range res.Recipe.Instructions {
		got = append(got, s.Title+"|"+s.Text)
	}
	want := []string{"Cook the rice|Simmer the rice in milk for 30 minutes.", "|serve it", "|Stir in the sugar.", "|Chill"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
}

func TestMicrodataStepTitleIsTheNameWhenItDiffersFromTheText(t *testing.T) {
	res := mustExtract(t, fixture(t, "microdata_category"), fixtureURL)
	var got []string
	for _, s := range res.Recipe.Instructions {
		got = append(got, s.Title+"|"+s.Text)
	}
	want := []string{"Brew|Steep the tea bags in hot water.", "|chill the tea"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("steps = %q, want %q", got, want)
	}
}
