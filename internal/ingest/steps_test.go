package ingest

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// recipeWith is pasted text whose ingredient lines all read well, so the only
// gaps a fallback can find are in the step graph.
func recipeWith(steps ...string) string {
	return "Bread\n\nIngredients\n2 cups flour\n1 cup water\n\nDirections\n" + strings.Join(steps, "\n") + "\n"
}

func flatSteps(d Draft) []Step {
	var out []Step
	for _, c := range d.Components {
		out = append(out, c.Steps...)
	}
	return out
}

func TestRulesChainStepsInReadingOrderByDefault(t *testing.T) { // ADR-00020 obligation 1, linear
	e := newEnv(t)
	got := flatSteps(ruleDraft(t, e, recipeWith("Mix the flour and water.", "Knead the dough.", "Shape the loaf.")))
	if len(got) != 3 {
		t.Fatalf("steps = %+v, want three", got)
	}
	wantDeps := [][]int{nil, {1}, {2}}
	for i, st := range got {
		if !reflect.DeepEqual(st.DependsOn, wantDeps[i]) || st.Passive || st.GraphCue {
			t.Errorf("step %d = deps %v passive %v cue %v, want deps %v, active, default", i+1, st.DependsOn, st.Passive, st.GraphCue, wantDeps[i])
		}
	}
}

func TestRulesMarkPassiveStepsFromTheVocabulary(t *testing.T) { // ADR-00020 obligation 1, passive
	e := newEnv(t)
	got := flatSteps(ruleDraft(t, e, recipeWith("Mix the dough.", "Let the dough rise for 1 hour.", "Bake for 30 minutes.", "Slice and serve.")))
	want := []struct{ passive, cue bool }{{false, false}, {true, true}, {true, true}, {false, false}}
	for i, st := range got {
		if st.Passive != want[i].passive || st.GraphCue != want[i].cue {
			t.Errorf("step %d (%q) = passive %v cue %v, want %v", i+1, st.Text, st.Passive, st.GraphCue, want[i])
		}
	}
	if !reflect.DeepEqual(got[3].DependsOn, []int{3}) {
		t.Errorf("the step after a passive step still follows it, got deps %v", got[3].DependsOn)
	}
}

func TestMeanwhileBreaksTheChain(t *testing.T) { // ADR-00020 obligation 1, "meanwhile"
	e := newEnv(t)
	got := flatSteps(ruleDraft(t, e, recipeWith("Roast the potatoes for 30 minutes.", "Meanwhile, make the sauce.", "Serve both together.")))
	if len(got) != 3 {
		t.Fatalf("steps = %+v", got)
	}
	if len(got[1].DependsOn) != 0 || !got[1].GraphCue {
		t.Errorf("a meanwhile step runs alongside the one before it: deps %v cue %v", got[1].DependsOn, got[1].GraphCue)
	}
	if !reflect.DeepEqual(got[2].DependsOn, []int{1, 2}) {
		t.Errorf("the step after joins both branches, got deps %v", got[2].DependsOn)
	}
}

func TestStepCuesNeedTheWordInTheRightPlace(t *testing.T) {
	tests := []struct {
		text              string
		passive, parallel bool
	}{
		// Not passive: the word names an ingredient, a tool, or another action.
		{"Add the rest of the flour.", false, false},
		{"Grease the baking dish.", false, false},
		{"Let the butter melt, then add the rest of the flour.", false, false},
		{"Let the oven preheat, then set the timer.", false, false},
		{"Leave the lid on and stir the stand mixer.", false, false},
		{"Let the pan and then cool for 5 minutes.", false, false},
		{"Set aside the bowl.", false, false},
		{"Add the roasted red peppers and the baked beans.", false, false},
		{"Stir in the freeze-dried strawberries.", false, false},
		{"Add the roast beef.", false, false},
		{"Add the sugar, freeze-dried strawberries and nuts.", false, false},
		{"Leave the bowl on the stand mixer.", false, false},
		{"Stir while it simmers.", false, false},
		{"Stir the simmering sauce.", false, false},
		// Passive: the instruction itself.
		{"Bake until golden.", true, false},
		{"Preheat the oven and bake for 20 minutes.", true, false},
		{"Marinate the chicken overnight.", true, false},
		{"Simmer for 20 minutes.", true, false},
		{"Gently simmer for 20 minutes.", true, false},
		{"Cool on a wire rack.", true, false},
		{"Leave the dough to rest.", true, false},
		{"Let the dough rise for 1 hour.", true, false},
		{"Let the jelly set overnight.", true, false},
		// Parallel only at the start of the step.
		{"Whisk the eggs. Meanwhile, heat the pan.", false, false},
		{"Meanwhile, heat the pan.", false, true},
		{"While the oven heats, chop the onions.", false, true},
		{"In the meantime, set the table.", false, true},
		{"Stir constantly while the sauce thickens.", false, false},
	}
	for _, tt := range tests {
		if got := isPassive(tt.text); got != tt.passive {
			t.Errorf("passive(%q) = %v, want %v", tt.text, got, tt.passive)
		}
		if got := parallelRE.MatchString(tt.text); got != tt.parallel {
			t.Errorf("parallel(%q) = %v, want %v", tt.text, got, tt.parallel)
		}
	}
}

func graphAnswer(entries ...StructuredStep) Structured {
	return Structured{
		Name:       "Bread",
		Components: []StructuredComponent{{Ingredients: []string{}}},
		StepGraph:  entries,
	}
}

func TestFallbackAsksAboutDefaultStepsOnly(t *testing.T) { // ADR-00020 decision 1
	e := newEnv(t)
	rule := ruleDraft(t, e, recipeWith("Mix the flour and water.", "Bake for 30 minutes.", "Slice and serve."))
	fp := &fakeProvider{resp: graphAnswer()}
	if _, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule); err != nil {
		t.Fatal(err)
	}
	var asked []int
	for _, g := range fp.calls[0].Gaps.Steps {
		asked = append(asked, g.Number)
	}
	if !reflect.DeepEqual(asked, []int{1, 3}) {
		t.Errorf("asked about steps %v, want the default steps 1 and 3 (2 is cue-backed)", asked)
	}
	text := fp.calls[0].Gaps.Describe()
	for _, want := range []string{"1. Mix the flour and water.", "2. Bake for 30 minutes.", "3. Slice and serve."} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered gaps lack the numbered step %q:\n%s", want, text)
		}
	}
}

func TestFallbackChangesADefaultStepAndLeavesACueBackedOne(t *testing.T) { // ADR-00020 obligation 2
	e := newEnv(t)
	rule := ruleDraft(t, e, recipeWith("Mix the flour and water.", "Bake for 30 minutes.", "Slice and serve."))
	ruleSteps := flatSteps(rule)
	fp := &fakeProvider{resp: graphAnswer(
		StructuredStep{Step: 3, Passive: true, DependsOn: []int{1}},
		StructuredStep{Step: 2, Passive: false, DependsOn: []int{}}, // the rules had a cue: ignored
	)}

	got, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	steps := flatSteps(got)
	if s := steps[2]; !s.Passive || !reflect.DeepEqual(s.DependsOn, []int{1}) || !s.GraphFromLLM || s.FromLLM {
		t.Errorf("default step = %+v, want the LLM's graph, marked, and still a rule step", s)
	}
	if !reflect.DeepEqual(steps[1], ruleSteps[1]) {
		t.Errorf("cue-backed step changed:\n got: %+v\nwant: %+v", steps[1], ruleSteps[1])
	}
	if !reflect.DeepEqual(steps[0], ruleSteps[0]) {
		t.Errorf("a default step the answer omitted must keep the rule graph, got %+v", steps[0])
	}
	if !reflect.DeepEqual(flatSteps(rule), ruleSteps) {
		t.Error("the caller's draft was modified")
	}
}

func TestFallbackWithAnInvalidStepGraphIsAnLLMFailure(t *testing.T) {
	e := newEnv(t)
	rule := ruleDraft(t, e, recipeWith("Mix the flour and water.", "Knead.", "Slice and serve."))
	for name, entries := range map[string][]StructuredStep{
		"dependency on a later step": {{Step: 2, DependsOn: []int{3}}},
		"dependency on itself":       {{Step: 2, DependsOn: []int{2}}},
		"step beyond the recipe":     {{Step: 9, DependsOn: []int{1}}},
		"step answered twice":        {{Step: 2, DependsOn: []int{1}}, {Step: 2, DependsOn: []int{1}}},
	} {
		_, err := e.pipelineWith(t, testConfig(), &fakeProvider{resp: graphAnswer(entries...)}).Fallback().Retry(e.ctx, rule)
		if !errors.Is(err, ErrLLMFailed) || !errors.Is(err, ErrInvalidStructured) {
			t.Errorf("%s: err = %v, want ErrLLMFailed wrapping ErrInvalidStructured", name, err)
		}
	}
}

func TestGapFillTakingTheComponentsBuildsTheGraphByRules(t *testing.T) {
	e := newEnv(t)
	rule := ruleDraft(t, e, "stew\n\nstir it\n")
	fp := &fakeProvider{resp: llmAnswer()} // steps: "Stir it all together.", "Simmer for 20 minutes."
	got, err := e.pipelineWith(t, testConfig(), fp).Fallback().Retry(e.ctx, rule)
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.calls[0].Gaps.Steps) != 0 {
		t.Errorf("step gaps = %+v: the answer's steps do not exist yet to be numbered", fp.calls[0].Gaps.Steps)
	}
	steps := flatSteps(got)
	if len(steps) != 2 || !steps[1].Passive || !reflect.DeepEqual(steps[1].DependsOn, []int{1}) {
		t.Errorf("steps = %+v, want the rules' graph over the LLM's steps", steps)
	}
}

func TestSavePersistsPassiveAndDependencies(t *testing.T) { // the review screen round-trips these (#230)
	e := newEnv(t)
	rule := ruleDraft(t, e, recipeWith("Roast the potatoes for 30 minutes.", "Meanwhile, make the sauce.", "Serve both together."))
	saved, err := e.saver().Save(e.ctx, confirm(t, rule))
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.store.Get(e.ctx, saved.RecipeID)
	if err != nil {
		t.Fatal(err)
	}
	steps := got.Components[0].Steps
	if len(steps) != 3 {
		t.Fatalf("steps = %+v", steps)
	}
	if !steps[0].Passive {
		t.Error("the roasting step must be saved passive")
	}
	if len(steps[1].DependsOn) != 0 {
		t.Errorf("meanwhile step deps = %v, want none", steps[1].DependsOn)
	}
	if want := []recipe.StepID{steps[0].ID, steps[1].ID}; !reflect.DeepEqual(steps[2].DependsOn, want) {
		t.Errorf("last step deps = %v, want %v", steps[2].DependsOn, want)
	}
}

func TestConfirmRejectsAStepDependencyOutsideTheDraft(t *testing.T) {
	e := newEnv(t)
	for name, deps := range map[string][]int{"unknown": {7}, "itself": {2}, "zero": {0}} {
		d := ruleDraft(t, e, recipeWith("Mix.", "Knead."))
		d.Components[0].Steps[1].DependsOn = deps
		if _, err := Confirm(d); !errors.Is(err, ErrIncompleteDraft) {
			t.Errorf("%s: err = %v, want ErrIncompleteDraft", name, err)
		}
	}
}
