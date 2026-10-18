package ingest

import (
	"os"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// The saved page of the owner's first test recipe, held with the extract
// fixtures: author by @id reference, servings in a bare yield, the video as a
// page embed, and blog cruft in the summary.
func TestIngestChickenCutletPageIsFaithful(t *testing.T) {
	page, err := os.ReadFile("extract/testdata/real_recipetineats_graph.html")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	srv, _ := serve(t, page)
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/chicken-cutlet")
	if err != nil {
		t.Fatal(err)
	}
	if d.Author.Value != "Yumiko" {
		t.Errorf("author = %q, want Yumiko", d.Author.Value)
	}
	if d.Servings.Value.String() != "4" {
		t.Errorf("servings = %v, want 4", d.Servings.Value)
	}
	if d.Yield.Value.Unit != "" || d.Yield.Confidence != 0 {
		t.Errorf("yield = %+v, want none beside servings", d.Yield)
	}
	if d.VideoID != "L57S_S22Mf8" {
		t.Errorf("video id = %q, want L57S_S22Mf8", d.VideoID)
	}
	want := "This is how Japanese people make a Chicken Cutlet that is deliciously crispy. " +
		"By deep-frying the cutlet, you can perfect the consistency of the golden-brown crumbs around the chicken. " +
		"Pour over fruity tonkatsu sauce to eat it. Cook Time assumes chicken is cooked in two batches."
	if d.Description.Value != want {
		t.Errorf("description = %q\nwant %q", d.Description.Value, want)
	}
	if d.ParserVersion != ParserVersion {
		t.Errorf("parser version = %q, want %q", d.ParserVersion, ParserVersion)
	}
}

// A yield with a unit still reads as a yield; only a bare number is servings.
func TestYieldKeepsAStatedUnit(t *testing.T) {
	for name, tc := range map[string]struct {
		yield          string
		servings, unit string
	}{
		"bare":     {"4", "4", ""},
		"servings": {"4 servings", "4", "servings"},
		"loaf":     {"1 loaf", "", "loaf"},
	} {
		t.Run(name, func(t *testing.T) {
			a := &assembler{d: &Draft{}}
			a.yield([]string{tc.yield})
			got := ""
			if a.d.Servings.Confidence > 0 {
				got = a.d.Servings.Value.String()
			}
			if got != tc.servings || a.d.Yield.Value.Unit != tc.unit {
				t.Errorf("servings %q yield unit %q, want %q and %q", got, a.d.Yield.Value.Unit, tc.servings, tc.unit)
			}
		})
	}
}

// A yield gap is filled by the same bare-number rule the rules use: an LLM
// answer of "4" is servings, and a draft with servings but no yield has no
// yield gap to ask about.
func TestFindGapsTreatsServingsAsAStatedYield(t *testing.T) {
	var d Draft
	if !findGaps(d, 0.8).Yield {
		t.Fatal("empty draft has no yield gap")
	}
	d.Servings = Field[quantity.Quantity]{Value: quantity.Quantity{}, Confidence: 1}
	if findGaps(d, 0.8).Yield {
		t.Error("a draft with servings still has a yield gap")
	}
}

func TestGapFillReadsABareYieldAsServings(t *testing.T) {
	e := newEnv(t)
	rule := ruleDraft(t, e, mixedText)
	// Everything but the yield is stated, so the yield is the only gap.
	full := func(s string) Field[string] { return Field[string]{Value: s, Confidence: 1} }
	rule.Name, rule.Author, rule.Description = full("Toast"), full("Ada"), full("Bread")
	rule.Prep.Confidence, rule.Cook.Confidence, rule.Total.Confidence = 1, 1, 1
	rule.Equipment = []Equipment{{Name: "toaster"}}
	for i := range rule.Components[0].Lines {
		rule.Components[0].Lines[i].Confidence = 1
	}
	rule.Yield, rule.Servings = Field[recipe.Yield]{}, Field[quantity.Quantity]{}
	ans := llmAnswer()
	ans.Yield = "4"
	fp := &fakeProvider{resp: ans}

	got, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if got.Servings.Value.String() != "4" || !got.Servings.FromLLM {
		t.Errorf("servings = %+v, want 4 from the LLM", got.Servings)
	}
	if got.Yield.Value.Unit != "" {
		t.Errorf("yield = %+v, want none beside servings", got.Yield)
	}
}
