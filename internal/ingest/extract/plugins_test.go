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

const notesPage = `<html><head><script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Katsu","recipeIngredient":["1 egg"],"recipeInstructions":[{"@type":"HowToStep","text":"Fry."}]}
</script></head><body>%s</body></html>`

func notesOf(t *testing.T, body string) []Note {
	t.Helper()
	res := mustExtract(t, strings.Replace(notesPage, "%s", body, 1), fixtureURL)
	if res.Recipe == nil {
		t.Fatalf("no recipe: %+v", res)
	}
	return res.Recipe.Notes
}

func TestPluginNotesWPRM(t *testing.T) {
	got := notesOf(t, `<div class="wprm-recipe-container"><div class="wprm-recipe-notes-container">
<div class="wprm-recipe-header">Recipe Notes</div>
<p><strong>Honey:</strong> Never give it to infants.</p>
<p>Keeps   three
days.</p><p>  </p><ul><li>Freezes well.</li></ul></div></div>`)
	want := []Note{
		{Title: "Honey", Text: "Never give it to infants."},
		{Text: "Keeps three days."},
		{Text: "Freezes well."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes = %+v, want %+v", got, want)
	}
}

// The Tasty Recipes markup is written from recollection, like the other
// plugin fixtures; a page that differs only fails to match.
func TestPluginNotesTasty(t *testing.T) {
	got := notesOf(t, `<div class="tasty-recipes-notes"><h3>Notes</h3><div class="tasty-recipes-notes-body">
<p>Use <em>cold</em> butter.</p><p>Swap the milk for oat milk.</p></div></div>`)
	want := []Note{{Text: "Use cold butter."}, {Text: "Swap the milk for oat milk."}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes = %+v, want %+v", got, want)
	}
}

func TestPluginNotesWithoutParagraphsIsOneNote(t *testing.T) {
	got := notesOf(t, `<div class="tasty-recipes-notes"><h3>Notes</h3>Keep it cold.<br>Serve fast.</div>`)
	want := []Note{{Text: "Keep it cold. Serve fast."}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes = %+v, want %+v", got, want)
	}
}

// Text outside a paragraph is a candidate too, whether bare, in a span or in a
// div of its own: ingest drops nothing (ADR-00041).
func TestPluginNotesKeepLooseTextBesideParagraphs(t *testing.T) {
	got := notesOf(t, `<div class="wprm-recipe-notes-container"><div class="wprm-recipe-header">Notes</div>
Bare text first.<p>A paragraph.</p><span>Span text</span> and <em>more</em><div>Div text.</div><p>Last one.</p></div>`)
	want := []Note{
		{Text: "Bare text first."},
		{Text: "A paragraph."},
		{Text: "Span text and more"},
		{Text: "Div text."},
		{Text: "Last one."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes = %+v, want %+v", got, want)
	}
}

func TestNoNotesBlockGivesNoNotes(t *testing.T) {
	if got := notesOf(t, `<p>Hello</p>`); got != nil {
		t.Errorf("notes = %+v, want none", got)
	}
}

// The RecipeTin Eats card keeps every paragraph of its notes block, the
// nutrition dump included: ingest drops nothing, and flags it instead.
func TestPluginNotesFromARealCard(t *testing.T) {
	res := mustExtract(t, fixture(t, "real_recipetineats_graph"), fixtureURL)
	notes := res.Recipe.Notes
	if len(notes) != 13 {
		t.Fatalf("got %d notes, want the 13 paragraphs of the block: %+v", len(notes), notes)
	}
	if !strings.HasPrefix(notes[0].Text, "1. I used chicken thigh fillets") {
		t.Errorf("first note = %q", notes[0].Text)
	}
	if last := notes[len(notes)-1].Text; !strings.HasPrefix(last, "serving: 209g calories: 566kcal") {
		t.Errorf("last note = %q, want the nutrition paragraph kept", last)
	}
	for _, n := range notes {
		if strings.Contains(n.Text, "Recipe Notes") {
			t.Errorf("the block's heading became a note: %q", n.Text)
		}
	}
}

// A WPRM card that holds the whole method in one li, a display-block span per
// numbered paragraph, is read as one step per block even though JSON-LD
// states a single step (#492).
func TestWPRMBlocksSplitASingleStatedStep(t *testing.T) {
	res := mustExtract(t, fixture(t, "real_wprm_numbered_block"), fixtureURL)
	got := res.Recipe.Instructions
	if len(got) != 4 {
		t.Fatalf("steps = %+v, want four", got)
	}
	if !strings.HasPrefix(got[0].Text, "1. In the bowl") || !strings.HasPrefix(got[3].Text, "4. Lightly dust") {
		t.Errorf("steps = %+v, want one per numbered paragraph, in order", got)
	}
}

func TestBlocksNeverSplitWhenWordsDiffer(t *testing.T) {
	page := strings.Replace(fixture(t, "real_wprm_numbered_block"), "a pinch of salt.", "a pinch of pepper.", 1)
	res := mustExtract(t, page, fixtureURL)
	if got := len(res.Recipe.Instructions); got != 1 {
		t.Errorf("steps = %d, want the stated one kept when the card disagrees", got)
	}
}

func TestParagraphChildrenSplitAStep(t *testing.T) {
	page := `<html><head><script type="application/ld+json">{"@type":"Recipe","name":"Rolls",
"recipeIngredient":["1 cup flour"],
"recipeInstructions":[{"@type":"HowToStep","text":"Mix the dough. Bake it."}]}</script></head>
<body><div class="tasty-recipes-instructions"><ol><li><p>Mix the dough.</p><p>Bake it.</p></li></ol></div></body></html>`
	res := mustExtract(t, page, fixtureURL)
	got := res.Recipe.Instructions
	if len(got) != 2 || got[0].Text != "Mix the dough." || got[1].Text != "Bake it." {
		t.Errorf("steps = %+v, want one per paragraph", got)
	}
}
