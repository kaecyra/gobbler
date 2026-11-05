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

// WPRM cards open a step with a bold title that schema.org flattens into the
// text (#391). The honey beer bread page states every step this way.
func TestWPRMBoldLeadsBecomeStepTitles(t *testing.T) {
	res := mustExtract(t, fixture(t, "real_wprm_step_titles"), fixtureURL)
	var titles []string
	for _, s := range res.Recipe.Instructions {
		titles = append(titles, s.Title)
	}
	want := []string{"Preheat oven", "Make the batter", "Add the butter", "Bake", "Serve"}
	if !reflect.DeepEqual(titles, want) {
		t.Errorf("titles = %q, want %q", titles, want)
	}
	if got := res.Recipe.Instructions[0].Text; got != "Heat oven to 350°F." {
		t.Errorf("first step text = %q, want the title taken off", got)
	}
}

func TestWPRMBoldLeadCases(t *testing.T) {
	const lead = `<strong>Preheat oven. </strong>Heat oven to 350°F.`
	for name, tc := range map[string]struct {
		card, title, text string
	}{
		"split":        {lead, "Preheat oven", "Heat oven to 350°F."},
		"colon":        {`<b>Preheat oven:</b> Heat oven to 350°F.`, "Preheat oven", "Heat oven to 350°F."},
		"wrapped":      {`<span><strong>Preheat oven.</strong> Heat oven to 350°F.</span>`, "Preheat oven", "Heat oven to 350°F."},
		"no bold":      {`Preheat oven. Heat oven to 350°F.`, "", "Preheat oven. Heat oven to 350°F."},
		"mid-sentence": {`Preheat oven. <strong>Heat oven</strong> to 350°F.`, "", "Preheat oven. Heat oven to 350°F."},
		"emphasis":     {`<strong>Preheat</strong> oven. Heat oven to 350°F.`, "", "Preheat oven. Heat oven to 350°F."},
		"all bold":     {`<strong>Preheat oven. Heat oven to 350°F.</strong>`, "", "Preheat oven. Heat oven to 350°F."},
	} {
		t.Run(name, func(t *testing.T) {
			page := strings.Replace(fixture(t, "real_wprm_step_titles"), lead, tc.card, 1)
			got := mustExtract(t, page, fixtureURL).Recipe.Instructions[0]
			if got.Title != tc.title || got.Text != tc.text {
				t.Errorf("step = %q / %q, want %q / %q", got.Title, got.Text, tc.title, tc.text)
			}
		})
	}
}

// A HowToStep name the page gives is kept over the card's bold lead.
func TestWPRMBoldLeadKeepsAStructuredTitle(t *testing.T) {
	page := strings.Replace(fixture(t, "real_wprm_step_titles"),
		`"name":"Preheat oven. Heat oven to 350°F."`, `"name":"Warm up"`, 1)
	got := mustExtract(t, page, fixtureURL).Recipe.Instructions[0]
	if got.Title != "Warm up" || got.Text != "Preheat oven. Heat oven to 350°F." {
		t.Errorf("step = %+v, want the HowToStep name and the whole text", got)
	}
}

// Tasty Recipes opens a step with the same bold lead WPRM does (#398); the
// reader is shared, so the lead rules are the ones TestWPRMBoldLeadCases pins.
func TestTastyBoldLeadsBecomeStepTitles(t *testing.T) {
	page := fixture(t, "plugin_tasty")
	page = strings.Replace(page, `"text":"Bake the cake."`, `"text":"Preheat oven. Bake the cake."`, 1)
	page = strings.Replace(page, `<li>Bake the cake.</li>`, `<li><strong>Preheat oven.</strong> Bake the cake.</li>`, 1)
	res := mustExtract(t, page, fixtureURL)
	got := res.Recipe.Instructions
	if len(got) != 2 || got[0].Title != "Preheat oven" || got[0].Text != "Bake the cake." || got[1].Title != "" {
		t.Errorf("steps = %+v, want the first titled Preheat oven with the lead off its text, the second untitled", got)
	}
}
