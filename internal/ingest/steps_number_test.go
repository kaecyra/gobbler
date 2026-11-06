package ingest

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
)

// A source's own step number is noise: gobbler numbers steps itself, in an
// order that can differ from the source's (#490). A leading quantity or time
// is part of the instruction and stays.
func TestStripStepNumber(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"dot", "1. Preheat the oven.", "Preheat the oven."},
		{"paren", "2) Mix the flour.", "Mix the flour."},
		{"wrapped", "(3) Fold in the eggs.", "Fold in the eggs."},
		{"colon", "4: Bake.", "Bake."},
		{"dash", "5 - Cool on a rack.", "Cool on a rack."},
		{"en dash", "5 – Cool on a rack.", "Cool on a rack."},
		{"step", "Step 1 Whisk the eggs.", "Whisk the eggs."},
		{"step colon", "Step 1: Whisk the eggs.", "Whisk the eggs."},
		{"step dot", "Step 2. Whisk the eggs.", "Whisk the eggs."},
		{"upper step dash", "STEP 3 – Whisk the eggs.", "Whisk the eggs."},
		{"lower step", "step 10: Serve.", "Serve."},
		{"two digits", "12. Serve warm.", "Serve warm."},
		{"quantity", "2 eggs go in next.", "2 eggs go in next."},
		{"decimal quantity", "1.5 cups of the batter go in the pan.", "1.5 cups of the batter go in the pan."},
		{"temperature", "350°F oven for the next part.", "350°F oven for the next part."},
		{"fraction", "1/2 the dough goes in the pan.", "1/2 the dough goes in the pan."},
		{"time", "10 minutes before serving, add the herbs.", "10 minutes before serving, add the herbs."},
		{"range", "1 - 2 minutes in the oven is enough.", "1 - 2 minutes in the oven is enough."},
		{"no space", "1.Preheat the oven.", "1.Preheat the oven."},
		{"mid text", "Preheat to 200. Then 3. Stir.", "Preheat to 200. Then 3. Stir."},
		{"only a number", "1.", "1."},
		{"only a step", "Step 2:", "Step 2:"},
		{"plain", "Stir well.", "Stir well."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripStepNumber(tc.in); got != tc.want {
				t.Errorf("stripStepNumber(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A numbered step must not split on the number: "1. Preheat oven. Heat..."
// reads its lead after the number is gone.
func TestAssembleStripsStepNumbers(t *testing.T) {
	e := newEnv(t)
	res := extract.Result{Method: extract.MethodJSONLD, Recipe: &extract.Raw{
		Name: "Bread", Ingredients: []string{"3 cups flour"},
		Instructions: []extract.InstructionStep{
			{Text: "1. Preheat oven. Heat oven to 350°F."},
			{Title: "Mix", Text: "Step 2: Whisk it."},
			{Text: "3) 2 eggs go in next."},
		},
	}}
	d, err := e.pipeline(t, nil).assemble(e.ctx, Source{}, res)
	if err != nil {
		t.Fatal(err)
	}
	got := flatSteps(d)
	if len(got) != 3 {
		t.Fatalf("steps = %+v, want three", got)
	}
	for i, want := range []struct{ title, text string }{
		{"Preheat oven", "Heat oven to 350°F."},
		{"Mix", "Whisk it."},
		{"", "2 eggs go in next."},
	} {
		if got[i].Title != want.title || got[i].Text != want.text {
			t.Errorf("step %d = %q / %q, want %q / %q", i+1, got[i].Title, got[i].Text, want.title, want.text)
		}
	}
}

// Steps the LLM fallback answers with are read the same way: a number the
// model copied from the source is stripped (#490).
func TestLLMFallbackStripsStepNumbers(t *testing.T) {
	e := newEnv(t)
	ans := llmAnswer()
	ans.Components[0].Steps = []string{"1. Preheat the oven.", "Step 2: Mix the batter.", "2 eggs go in last."}
	p := e.pipelineWith(t, testConfig(), &fakeProvider{resp: ans})

	d, err := p.FromText(e.ctx, messyText)
	if err != nil {
		t.Fatal(err)
	}
	got := flatSteps(d)
	want := []string{"Preheat the oven.", "Mix the batter.", "2 eggs go in last."}
	if len(got) != len(want) {
		t.Fatalf("steps = %+v, want %d", got, len(want))
	}
	for i, w := range want {
		if !got[i].FromLLM || got[i].Text != w {
			t.Errorf("step %d = %q (from LLM %v), want %q", i+1, got[i].Text, got[i].FromLLM, w)
		}
	}
}
