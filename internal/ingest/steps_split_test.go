package ingest

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
)

// A source that publishes one step holding several numbered ones is split at
// each enumerator, but only along a strictly ascending run from 1 (#492).
func TestSplitNumberedSteps(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
	}{
		{"split", "1. Mix the dough. 2. Rest it. 3. Bake it.",
			[]string{"1. Mix the dough.", "2. Rest it.", "3. Bake it."}},
		{"no space before the next number", "1. Fill the baking dish.2. Lightly dust it.",
			[]string{"1. Fill the baking dish.", "2. Lightly dust it."}},
		{"paren form", "1) Mix. 2) Bake.", []string{"1) Mix.", "2) Bake."}},
		{"single enumerator", "1. Mix the dough and bake it.", []string{"1. Mix the dough and bake it."}},
		{"not from one", "2. Mix. 3. Bake.", []string{"2. Mix. 3. Bake."}},
		{"mid text enumerators", "Bake 2. Then rest 3. Then serve.", []string{"Bake 2. Then rest 3. Then serve."}},
		{"quantity", "1. Add 1.5 cups of flour. Stir until smooth.", []string{"1. Add 1.5 cups of flour. Stir until smooth."}},
		{"quantity after enumerator", "1. Add 2 cups. 2. Stir.", []string{"1. Add 2 cups.", "2. Stir."}},
		{"descending", "1. Mix. 3. Bake. 2. Serve.", []string{"1. Mix. 3. Bake. 2. Serve."}},
		{"run stops at a gap", "1. Mix. 2. Bake. 4. Serve.", []string{"1. Mix.", "2. Bake. 4. Serve."}},
		{"range of minutes", "1 - 2 minutes in the oven. 2 - 3 minutes to rest.", []string{"1 - 2 minutes in the oven. 2 - 3 minutes to rest."}},
		{"bare word before a number", "1. Divide the dough into 2. Shape each half. 3. Bake.",
			[]string{"1. Divide the dough into 2. Shape each half. 3. Bake."}},
		{"sentence ends before each number", "1. Divide the dough into 2 halves! 2. Shape each? 3. Bake (see note) 4. Cool.",
			[]string{"1. Divide the dough into 2 halves!", "2. Shape each?", "3. Bake (see note)", "4. Cool."}},
		{"plain", "Stir well.", []string{"Stir well."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := splitNumberedSteps(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("splitNumberedSteps(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAssembleSplitsASourceStepHoldingSeveral(t *testing.T) {
	e := newEnv(t)
	res := extract.Result{Method: extract.MethodJSONLD, Recipe: &extract.Raw{
		Name: "Bread", Ingredients: []string{"3 cups flour"},
		Instructions: []extract.InstructionStep{
			{Text: "1. Mix the dough. Rest it for 10 minutes. 2. Bake for 30 minutes."},
			{Text: "Serve."},
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
	if got[0].Duration != 10*60e9 || got[1].Duration != 30*60e9 {
		t.Errorf("durations = %v, %v, want each split step timed on its own", got[0].Duration, got[1].Duration)
	}
	for i, s := range got {
		if strings.ContainsAny(s.Text[:1], "123456789") {
			t.Errorf("step %d = %q, want the number stripped", i+1, s.Text)
		}
	}
}

// The saved Parker House rolls page: one JSON-LD step, one WPRM li holding a
// block per numbered paragraph. The draft has a step for each (#492).
func TestIngestNumberedBlocksBecomeSteps(t *testing.T) {
	page, err := os.ReadFile("extract/testdata/real_wprm_numbered_block.html")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	srv, _ := serve(t, page)
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/rolls")
	if err != nil {
		t.Fatal(err)
	}
	got := flatSteps(d)
	if len(got) != 6 {
		t.Fatalf("steps = %+v, want six", got)
	}
	for i, s := range got {
		if s.Text == "" || s.Text[0] >= '0' && s.Text[0] <= '9' {
			t.Errorf("step %d = %q, want the number stripped", i+1, s.Text)
		}
	}
	// A lead sentence may have become the title, so each step is its title and
	// text together.
	for i, want := range []string{"In the bowl", "To make the honey butter", "Preheat the oven", "Lightly dust", "Cover the dish", "Bake the rolls"} {
		if text := strings.TrimSpace(got[i].Title + " " + got[i].Text); !strings.HasPrefix(text, want) {
			t.Errorf("step %d = %q, want it to open %q", i+1, text, want)
		}
	}
}

// A titled source step that holds several gives the title to the first piece
// only; the others would repeat it (#492).
func TestAssembleKeepsATitleOnTheFirstPieceOnly(t *testing.T) {
	e := newEnv(t)
	res := extract.Result{Method: extract.MethodJSONLD, Recipe: &extract.Raw{
		Name: "Bread", Ingredients: []string{"3 cups flour"},
		Instructions: []extract.InstructionStep{{Title: "Make the bread", Text: "1. Mix the dough. 2. Bake it."}},
	}}
	d, err := e.pipeline(t, nil).assemble(e.ctx, Source{}, res)
	if err != nil {
		t.Fatal(err)
	}
	got := flatSteps(d)
	if len(got) != 2 || got[0].Title != "Make the bread" || got[1].Title == "Make the bread" {
		t.Errorf("steps = %+v, want the title on the first piece only", got)
	}
}

// Pasted text goes through the same split (#492, #502). The section parser
// keeps a "1." marker whose line holds the rest of the run, so the split sees
// the run from 1 in every form.
func TestAssembleSplitsNumberedStepsInPastedText(t *testing.T) {
	for _, line := range []string{
		"(1) Mix the flour and salt. (2) Bake for 30 minutes. (3) Serve warm.",
		"1. Mix the flour and salt. 2. Bake for 30 minutes. 3. Serve warm.",
		"1) Mix the flour and salt. 2) Bake for 30 minutes. 3) Serve warm.",
		"1. Mix the flour and salt. (2) Bake for 30 minutes. (3) Serve warm.",
	} {
		t.Run(line[:3], func(t *testing.T) { assertPastedSplit(t, line) })
	}
}

func assertPastedSplit(t *testing.T, line string) {
	t.Helper()
	e := newEnv(t)
	res := extract.Result{Method: extract.MethodText, Text: "Bread\n\nIngredients\n3 cups flour\n1 tsp salt\n\nInstructions\n" + line + "\n"}
	d, err := e.pipeline(t, nil).assemble(e.ctx, Source{}, res)
	if err != nil {
		t.Fatal(err)
	}
	got := flatSteps(d)
	if len(got) != 3 {
		t.Fatalf("steps = %+v, want three", got)
	}
	for i, s := range got {
		if strings.ContainsAny(s.Text[:1], "123456789") {
			t.Errorf("step %d = %q, want the number stripped", i+1, s.Text)
		}
	}
}

// A numbered list on separate lines still gives one step per line.
func TestAssemblePastedNumberedLinesStaySeparate(t *testing.T) {
	e := newEnv(t)
	res := extract.Result{Method: extract.MethodText, Text: "Bread\n\nIngredients\n3 cups flour\n\nInstructions\n1. Mix.\n2. Bake.\n"}
	d, err := e.pipeline(t, nil).assemble(e.ctx, Source{}, res)
	if err != nil {
		t.Fatal(err)
	}
	got := flatSteps(d)
	if len(got) != 2 || got[0].Text != "Mix." || got[1].Text != "Bake." {
		t.Errorf("steps = %+v, want Mix. and Bake.", got)
	}
}

// Pasted lines that must not split, and the multi-line shapes (#502). Each
// case lists the step texts, numbers removed.
func TestAssemblePastedNumberedEdgeCases(t *testing.T) {
	cases := []struct {
		name, steps string
		want        []string
	}{
		{"quantity after a number", "1. Preheat oven to 350. 2 eggs go in later",
			[]string{"Preheat oven to 350. 2 eggs go in later"}},
		{"bare word before a number", "1. Bake for 2. Then cool",
			[]string{"Bake for 2. Then cool"}},
		{"empty tail", "1. Mix. 2.", []string{"Mix."}},
		{"bare marker line then the run", "1.\nMix. 2. Bake. 3. Serve.",
			[]string{"Mix.", "Bake.", "Serve."}},
		{"bare marker line then a line", "1.\nMix.\n2.\nBake.", []string{"Mix.", "Bake."}},
		{"space before the point", "1 . Mix the flour well.\n2 . Bake it.",
			[]string{"Mix the flour well.", "Bake it."}},
		{"space before the paren", "1 ) Mix the flour well.\n2 ) Bake it.",
			[]string{"Mix the flour well.", "Bake it."}},
		{"space then decimal quantity", "1 .5 cups of flour go in first",
			[]string{"1 .5 cups of flour go in first"}},
		// Line 1 holds a run of two and line 2 starts a new line at 3: the
		// first line splits, the second is its own step.
		{"run then a numbered line", "1. Mix. 2. Rest.\n3. Serve.",
			[]string{"Mix.", "Rest.", "Serve."}},
		{"separate numbered lines", "1. Mix.\n2. Bake.", []string{"Mix.", "Bake."}},
		{"bare number line", "1\nMix.\n2\nBake.", []string{"Mix.", "Bake."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			res := extract.Result{Method: extract.MethodText, Text: "Bread\n\nIngredients\n3 cups flour\n\nInstructions\n" + tc.steps + "\n"}
			d, err := e.pipeline(t, nil).assemble(e.ctx, Source{}, res)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, s := range flatSteps(d) {
				got = append(got, s.Text)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("steps = %q, want %q", got, tc.want)
			}
		})
	}
}

// A spaced enumerator ("1 . Mix") is stripped like an unspaced one, and a
// quantity or time is left alone (#502).
func TestStripStepNumberSpaced(t *testing.T) {
	for in, want := range map[string]string{
		"1 . Mix":          "Mix",
		"1 ) Mix":          "Mix",
		"1 : Mix":          "Mix",
		"1 .5 cups":        "1 .5 cups",
		"1.5 cups":         "1.5 cups",
		"350\u00b0F oven":  "350\u00b0F oven",
		"1 - 2 minutes":    "1 - 2 minutes",
		"1. Mix. 2.":       "Mix.",
		"1. Bake at 350.":  "Bake at 350.",
		"1. Add salt. 2 .": "Add salt.",
	} {
		if got := stripStepNumber(in); got != want {
			t.Errorf("stripStepNumber(%q) = %q, want %q", in, got, want)
		}
	}
}
