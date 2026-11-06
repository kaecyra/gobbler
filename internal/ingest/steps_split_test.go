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
	if len(got) != 4 {
		t.Fatalf("steps = %+v, want four", got)
	}
	for i, s := range got {
		if s.Text == "" || s.Text[0] >= '0' && s.Text[0] <= '9' {
			t.Errorf("step %d = %q, want the number stripped", i+1, s.Text)
		}
	}
	if !strings.Contains(got[1].Text, "honey butter") || !strings.Contains(got[3].Text, "bake for 25 minutes") {
		t.Errorf("steps out of order: %+v", got)
	}
}
