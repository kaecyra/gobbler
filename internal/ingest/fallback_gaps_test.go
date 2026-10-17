package ingest

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// mixedText has one readable ingredient line and one the line parser cannot
// read, under real headings: the rules read it well except for that line.
const mixedText = "Toast\n\nIngredients\n2 slices bread\n?? a good amount of glorp #@!\n\nDirections\nToast the bread.\n"

const (
	goodLine = "2 slices bread"
	badLine  = "?? a good amount of glorp #@!"
)

// ruleDraft reads text with no provider, so the draft is purely rule-based.
func ruleDraft(t *testing.T, e *env, text string) Draft {
	t.Helper()
	d, err := e.pipelineWith(t, testConfig(), nil).FromText(e.ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// gapAnswer is a provider answer for mixedText that disagrees with the rules
// about everything it is able to: the name, the steps and the good line's
// component, as well as giving the one replacement that was asked for.
func gapAnswer(replacement string) Structured {
	return Structured{
		Name:   "Something Else Entirely",
		Author: "Ada",
		Components: []StructuredComponent{{
			Ingredients: []string{replacement},
			Steps:       []string{"Do something completely different."},
		}},
	}
}

func lineByRaw(t *testing.T, d Draft, raw string) Line {
	t.Helper()
	for _, c := range d.Components {
		for _, l := range c.Lines {
			if l.Raw == raw {
				return l
			}
		}
	}
	t.Fatalf("no line %q in %+v", raw, d.Components)
	return Line{}
}

func TestGapFillRequestHoldsOnlyTheGaps(t *testing.T) { // ADR-00020 obligation 3
	e := newEnv(t)
	rule := ruleDraft(t, e, mixedText)
	if got := lineByRaw(t, rule, goodLine); got.Confidence < testConfig().ConfidenceThreshold {
		t.Fatalf("precondition: %q has confidence %v, want at or above the threshold", goodLine, got.Confidence)
	}
	if got := lineByRaw(t, rule, badLine); got.Confidence >= testConfig().ConfidenceThreshold {
		t.Fatalf("precondition: %q has confidence %v, want below the threshold", badLine, got.Confidence)
	}

	fp := &fakeProvider{resp: gapAnswer("1 pinch glorp")}
	if _, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule); err != nil {
		t.Fatal(err)
	}
	req := fp.calls[0]
	if len(req.Gaps.Lines) != 1 || req.Gaps.Lines[0].Raw != badLine {
		t.Errorf("gap lines = %+v, want only %q", req.Gaps.Lines, badLine)
	}
	if req.Gaps.Components || req.Gaps.Name {
		t.Errorf("gaps = %+v: the components and the name met the threshold", req.Gaps)
	}
	if !reflect.DeepEqual(req.RuleDraft, Draft{}) {
		t.Errorf("the whole rule-based draft was sent: %+v", req.RuleDraft)
	}
	if text := req.Gaps.Describe(); strings.Contains(text, goodLine) || !strings.Contains(text, badLine) {
		t.Errorf("rendered gaps must carry the weak line and not the good one:\n%s", text)
	}
}

func TestGapFillKeepsEveryRuleValueThatMetTheThreshold(t *testing.T) { // ADR-00020 obligation 3
	e := newEnv(t)
	rule := ruleDraft(t, e, mixedText)
	fp := &fakeProvider{resp: gapAnswer("1 pinch glorp")}

	got, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if want := lineByRaw(t, rule, goodLine); !reflect.DeepEqual(lineByRaw(t, got, goodLine), want) {
		t.Errorf("high-confidence line changed:\n got: %+v\nwant: %+v", lineByRaw(t, got, goodLine), want)
	}
	if got.Name != rule.Name {
		t.Errorf("name = %+v, want the rule value %+v: it met the threshold", got.Name, rule.Name)
	}
	if !reflect.DeepEqual(got.Components[0].Steps, rule.Components[0].Steps) {
		t.Errorf("steps = %+v, want the rule steps %+v", got.Components[0].Steps, rule.Components[0].Steps)
	}
	if !got.UsedLLM {
		t.Error("the draft must say the LLM contributed")
	}
	if string(got.Source.Content) != mixedText || got.Source.SHA256 != rule.Source.SHA256 {
		t.Error("the source snapshot must be unchanged")
	}
}

func TestGapFillReplacesTheWeakLineAndFillsMissingFields(t *testing.T) {
	e := newEnv(t)
	rule := ruleDraft(t, e, mixedText)
	if rule.Author.Confidence != 0 {
		t.Fatalf("precondition: the rules read no author, got %+v", rule.Author)
	}
	fp := &fakeProvider{resp: gapAnswer("1 pinch glorp")}

	got, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Components[0].Lines) != 2 {
		t.Fatalf("lines = %+v, want the same two", got.Components[0].Lines)
	}
	repl := got.Components[0].Lines[1]
	if repl.Raw != "1 pinch glorp" || !repl.FromLLM {
		t.Errorf("weak line = %+v, want the LLM's replacement marked FromLLM", repl)
	}
	if got.Components[0].Lines[0].FromLLM {
		t.Error("the kept line must not be marked FromLLM")
	}
	if got.Author.Value != "Ada" || !got.Author.FromLLM {
		t.Errorf("author = %+v, want the LLM's, marked", got.Author)
	}
	if got.Components[0].Lines[1].Confidence <= rule.Components[0].Lines[1].Confidence {
		t.Error("the replacement is parsed like any line and should read better than the unreadable one")
	}
}

func TestGapFillReplacesALowConfidenceField(t *testing.T) {
	e := newEnv(t)
	rule := ruleDraft(t, e, mixedText)
	cfg := testConfig()
	cfg.ConfidenceThreshold = 0.9 // the title of a text source is stated at 0.8
	if rule.Name.Confidence >= cfg.ConfidenceThreshold || rule.Name.Confidence == 0 {
		t.Fatalf("precondition: name confidence %v must be low but present", rule.Name.Confidence)
	}
	fp := &fakeProvider{resp: gapAnswer("1 pinch glorp")}
	got, err := e.pipelineWith(t, cfg, fp).Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if !fp.calls[0].Gaps.Name {
		t.Error("a name below the threshold must be a gap")
	}
	if got.Name.Value != "Something Else Entirely" || !got.Name.FromLLM {
		t.Errorf("name = %+v, want the LLM's, marked", got.Name)
	}
}

func TestGapFillWithAWrongNumberOfLinesIsAnLLMFailure(t *testing.T) {
	e := newEnv(t)
	rule := ruleDraft(t, e, mixedText)
	ans := gapAnswer("1 pinch glorp")
	ans.Components[0].Ingredients = []string{"1 pinch glorp", "2 more things"}

	_, err := e.pipelineWith(t, testConfig(), &fakeProvider{resp: ans}).Fallback().Retry(e.ctx, rule)
	if !errors.Is(err, ErrLLMFailed) || !errors.Is(err, ErrInvalidStructured) {
		t.Fatalf("err = %v, want ErrLLMFailed wrapping ErrInvalidStructured", err)
	}
}

func TestGapFillDoesNotMutateTheRuleDraft(t *testing.T) {
	e := newEnv(t)
	rule := ruleDraft(t, e, mixedText)
	snapshot := ruleDraft(t, e, mixedText)
	if _, err := e.pipelineWith(t, testConfig(), &fakeProvider{resp: gapAnswer("1 pinch glorp")}).Fallback().Retry(e.ctx, rule); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rule, snapshot) {
		t.Errorf("the caller's draft was modified:\n got: %+v\nwant: %+v", rule, snapshot)
	}
}

func TestGapFillTakesTheWholeRecipeWhenTheRulesFoundNoIngredients(t *testing.T) {
	e := newEnv(t)
	rule := ruleDraft(t, e, "stew\n\nstir it\n")
	for _, c := range rule.Components {
		if len(c.Lines) > 0 {
			t.Fatalf("precondition: no lines, got %+v", rule.Components)
		}
	}
	fp := &fakeProvider{resp: llmAnswer()}
	got, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if !fp.calls[0].Gaps.Components || len(fp.calls[0].Gaps.Lines) != 0 {
		t.Errorf("gaps = %+v, want Components and no lines", fp.calls[0].Gaps)
	}
	if len(got.Components) != 1 || len(got.Components[0].Lines) != 2 || !got.Components[0].Lines[0].FromLLM {
		t.Errorf("components = %+v, want the LLM's", got.Components)
	}
}

func TestGapFillKeepsRuleStepsThatMetTheThresholdWhenTakingTheComponents(t *testing.T) { // ADR-00020 decision 2
	e := newEnv(t)
	rule := ruleDraft(t, e, "Toast\n\nIngredients\n\nDirections\nToast the bread.\n")
	var steps []Step
	for _, c := range rule.Components {
		if len(c.Lines) > 0 {
			t.Fatalf("precondition: no lines, got %+v", rule.Components)
		}
		steps = append(steps, c.Steps...)
	}
	cfg := testConfig()
	cfg.ConfidenceThreshold = 0.5 // a split without a steps heading scores its steps 0.6
	if len(steps) == 0 || steps[0].Confidence < cfg.ConfidenceThreshold {
		t.Fatalf("precondition: a rule step that met the threshold, got %+v", steps)
	}
	got, err := e.pipelineWith(t, cfg, &fakeProvider{resp: llmAnswer()}).Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Components) != 1 || len(got.Components[0].Lines) != 2 {
		t.Fatalf("components = %+v, want the LLM's lines", got.Components)
	}
	if !reflect.DeepEqual(got.Components[0].Steps, steps) {
		t.Errorf("steps = %+v, want the rule steps %+v", got.Components[0].Steps, steps)
	}
}

func TestRetryWithNothingToFillMakesNoCall(t *testing.T) {
	e := newEnv(t)
	rule := ruleDraft(t, e, mixedText)
	// Everything is stated, with full confidence.
	full := func(s string) Field[string] { return Field[string]{Value: s, Confidence: 1} }
	rule.Name, rule.Author, rule.Description = full("Toast"), full("Ada"), full("Bread")
	rule.Yield.Confidence, rule.Prep.Confidence, rule.Cook.Confidence, rule.Total.Confidence = 1, 1, 1, 1
	rule.Equipment = []Equipment{{Name: "toaster"}}
	rule.Components[0].Lines[1].Confidence = 1
	fp := &fakeProvider{resp: llmAnswer()}

	got, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if fp.count() != 0 {
		t.Errorf("provider calls = %d, want none: there is nothing to fill", fp.count())
	}
	if !reflect.DeepEqual(got, rule) {
		t.Error("the draft must come back unchanged")
	}
}

func TestGapsDescribeAndEmpty(t *testing.T) {
	if !(Gaps{}).Empty() {
		t.Error("zero Gaps must be Empty")
	}
	g := Gaps{Prep: true, Steps: []GapStep{{Number: 1, Text: "Mix."}}, StepTexts: []string{"Mix."}}
	if g.Empty() {
		t.Error("Gaps with a field or a step is not Empty")
	}
	if text := g.Describe(); !strings.Contains(text, "prep_time") || !strings.Contains(text, "Mix.") {
		t.Errorf("Describe = %q", text)
	}
}
