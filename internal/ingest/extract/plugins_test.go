package extract

import (
	"reflect"
	"strings"
	"testing"
)

func groupsOf(r *Raw) map[string][]string {
	out := map[string][]string{}
	for _, g := range r.IngredientGroups {
		out[g.Name] = g.Lines
	}
	return out
}

func TestPluginIngredientGroups(t *testing.T) {
	cases := []struct {
		fixture string
		method  Method
		want    []IngredientGroup
	}{
		{"plugin_wprm", MethodJSONLD, []IngredientGroup{
			{"For the dough", []string{"3 cups flour", "1 tsp salt"}},
			{"For the filling", []string{"1 lb pork, minced", "2 tbsp soy sauce"}},
		}},
		{"plugin_tasty", MethodJSONLD, []IngredientGroup{
			{"Cake", []string{"2 cups flour", "1 cup sugar"}},
			{"Frosting", []string{"1 cup butter", "2 cups icing sugar"}},
		}},
		{"plugin_create", MethodMicrodata, []IngredientGroup{
			{"Rice", []string{"1 cup rice", "2 cups water"}},
			{"Topping", []string{"1 egg"}},
		}},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			res := mustExtract(t, fixture(t, c.fixture), fixtureURL)
			if res.Method != c.method || res.Recipe == nil {
				t.Fatalf("method %q recipe %v", res.Method, res.Recipe)
			}
			if !reflect.DeepEqual(res.Recipe.IngredientGroups, c.want) {
				t.Errorf("groups = %+v, want %+v", res.Recipe.IngredientGroups, c.want)
			}
		})
	}
}

func TestPluginStepSections(t *testing.T) {
	for fixtureName, want := range map[string][]string{
		"plugin_wprm":  {"Dough", "Filling", "Finish"},
		"plugin_tasty": {"Cake", "Frosting"},
	} {
		res := mustExtract(t, fixture(t, fixtureName), fixtureURL)
		var got []string
		for _, s := range res.Recipe.Instructions {
			got = append(got, s.Section)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: sections = %v, want %v", fixtureName, got, want)
		}
	}
}

// Groups that disagree with the structured data are not trusted: the recipe
// stays flat rather than regrouping lines the page did not group.
func TestPluginDisagreementStaysFlat(t *testing.T) {
	page := strings.Replace(fixture(t, "plugin_wprm"), "3 cups flour", "4 cups rye", 1)
	res := mustExtract(t, page, fixtureURL)
	if res.Recipe.IngredientGroups != nil {
		t.Errorf("groups = %+v, want none", res.Recipe.IngredientGroups)
	}
	if len(res.Recipe.Ingredients) != 4 {
		t.Errorf("flat ingredients lost: %v", res.Recipe.Ingredients)
	}
}

func TestPluginDifferentLineCountStaysFlat(t *testing.T) {
	page := strings.Replace(fixture(t, "plugin_tasty"), `"1 cup butter",`, ``, 1)
	res := mustExtract(t, page, fixtureURL)
	if res.Recipe.IngredientGroups != nil {
		t.Errorf("groups = %+v, want none", res.Recipe.IngredientGroups)
	}
}

// A plugin card with a single unnamed group adds nothing.
func TestPluginUnnamedGroupIgnored(t *testing.T) {
	page := strings.Replace(fixture(t, "plugin_wprm"), "For the dough", "", 1)
	page = strings.Replace(page, "For the filling", "", 1)
	res := mustExtract(t, page, fixtureURL)
	if g := groupsOf(res.Recipe); len(g) != 0 {
		t.Errorf("groups = %v, want none", g)
	}
}

// The structured step's title survives the plugin naming its section.
func TestPluginSectionsKeepStepTitles(t *testing.T) {
	page := strings.Replace(fixture(t, "plugin_wprm"),
		`{"@type":"HowToStep","text":"Mix the dough."}`,
		`{"@type":"HowToStep","name":"Make the dough","text":"Mix the dough."}`, 1)
	res := mustExtract(t, page, fixtureURL)
	if got := res.Recipe.Instructions[0]; got.Section != "Dough" || got.Title != "Make the dough" {
		t.Errorf("first step = %+v, want section Dough and title Make the dough", got)
	}
}

func TestPluginDoesNotOverrideJSONLDSections(t *testing.T) {
	page := strings.Replace(fixture(t, "plugin_wprm"),
		`{"@type":"HowToStep","text":"Mix the dough."}`,
		`{"@type":"HowToSection","name":"Own","itemListElement":[{"@type":"HowToStep","text":"Mix the dough."}]}`, 1)
	res := mustExtract(t, page, fixtureURL)
	if got := res.Recipe.Instructions[0].Section; got != "Own" {
		t.Errorf("section = %q, want the JSON-LD one", got)
	}
}
