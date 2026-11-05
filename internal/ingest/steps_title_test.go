package ingest

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// A step that opens with a short lead sentence and goes on is read as a title
// and an instruction (#398). The rule is deliberately narrow: a wrong split
// costs a mangled step, and a missed one only an empty title.
func TestSplitLead(t *testing.T) {
	cases := []struct {
		name, text, title, rest string
	}{
		{"lead sentence", "Preheat oven. Heat the oven to 375F and butter a loaf pan.", "Preheat oven", "Heat the oven to 375F and butter a loaf pan."},
		{"colon lead", "Glaze: brush the top.", "Glaze", "brush the top."},
		{"five words", "Mix the wet ingredients well. Fold in flour.", "Mix the wet ingredients well", "Fold in flour."},
		{"single sentence", "Cool on a rack before slicing.", "", ""},
		{"lead with a number", "Bake 50 minutes. Cool in the pan.", "", ""},
		{"lead with a fraction", "Add ½ cup. Stir well.", "", ""},
		{"six words", "Mix the wet ingredients very well. Fold in flour.", "", ""},
		{"nothing follows", "Serve.", "", ""},
		{"lead then only space", "Serve.   ", "", ""},
		{"no space after the stop", "Stir 1.5 cups of milk in.", "", ""},
		{"question", "Hungry? Eat. Enjoy the bread.", "", ""},
		{"empty", "", "", ""},
		{"stop first", ". Mix.", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			title, rest, ok := splitLead(tc.text)
			if title != tc.title || rest != tc.rest || ok != (tc.title != "") {
				t.Errorf("splitLead(%q) = %q, %q, %v; want %q, %q", tc.text, title, rest, ok, tc.title, tc.rest)
			}
		})
	}
}

// Pasted text and JSON-LD both run the title into the instruction. The split
// is flagged low confidence so the review screen shows it as an item to check.
func TestAssembleSplitsLeadSentencesAndFlagsThem(t *testing.T) {
	e := newEnv(t)
	res := extract.Result{Method: extract.MethodJSONLD, Recipe: &extract.Raw{
		Name: "Honey Beer Bread", Ingredients: []string{"3 cups flour"},
		Instructions: []extract.InstructionStep{
			{Text: "Preheat oven. Heat oven to 350°F."},
			{Title: "Make the batter", Text: "Whisk it."},
			{Text: "Cool on a rack before slicing."},
			{Text: "Bake. Bake for 50 minutes."},
			{Text: "Serve. Slice and enjoy."},
		},
	}}
	d, err := e.pipeline(t, nil).assemble(e.ctx, Source{}, res)
	if err != nil {
		t.Fatal(err)
	}
	got := flatSteps(d)
	if len(got) != 5 {
		t.Fatalf("steps = %+v, want five", got)
	}
	for i, want := range []struct {
		title, text string
		low         bool
		kind        recipe.StageKind
	}{
		{"Preheat oven", "Heat oven to 350°F.", true, ""},
		{"Make the batter", "Whisk it.", false, ""},
		{"", "Cool on a rack before slicing.", false, ""},
		{"Bake", "Bake for 50 minutes.", true, recipe.StageBake},
		{"Serve", "Slice and enjoy.", true, recipe.StageServe},
	} {
		s := got[i]
		if s.Title != want.title || s.Text != want.text || (s.Confidence < d.Threshold) != want.low || s.Kind != want.kind {
			t.Errorf("step %d = title %q text %q conf %v kind %q; want %q %q low=%v kind %q",
				i+1, s.Title, s.Text, s.Confidence, s.Kind, want.title, want.text, want.low, want.kind)
		}
	}
}

func TestPastedTextSplitsLeadSentences(t *testing.T) {
	e := newEnv(t)
	d := ruleDraft(t, e, recipeWith(
		"1. Preheat oven. Heat the oven to 375F and butter a loaf pan.",
		"2. Make the batter. Whisk the flour and beer.",
		"3. Serve. Slice with a bread knife.",
		"4. Cool on a rack before slicing."))
	got := flatSteps(d)
	if len(got) != 4 {
		t.Fatalf("steps = %+v, want four", got)
	}
	wantTitles := []string{"Preheat oven", "Make the batter", "Serve", ""}
	for i, s := range got {
		low := s.Confidence < d.Threshold
		if s.Title != wantTitles[i] || low != (wantTitles[i] != "") {
			t.Errorf("step %d = title %q conf %v, want title %q flagged=%v", i+1, s.Title, s.Confidence, wantTitles[i], wantTitles[i] != "")
		}
	}
	if got[2].Kind != recipe.StageServe {
		t.Errorf("split title Serve gave kind %q, want serve", got[2].Kind)
	}
}
