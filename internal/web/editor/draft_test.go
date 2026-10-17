package editor

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func twoComponents() draft {
	return draft{
		Components: []compDraft{
			{Name: "A", Steps: []stepDraft{{ID: "a1", Text: "one", To: 0}, {ID: "a2", Text: "two", To: 0, Deps: []string{"a1"}}}},
			{Name: "B", Steps: []stepDraft{{ID: "b1", Text: "three", To: 1, Deps: []string{"a2"}}}},
		},
		Equipment: []equipDraft{{Name: "whisk", Steps: []string{"a1", "b1"}}},
	}
}

func stepIDsOf(c compDraft) []string {
	var out []string
	for _, s := range c.Steps {
		out = append(out, s.ID)
	}
	return out
}

func TestArrangeMovesAStepToAnotherComponent(t *testing.T) {
	d := twoComponents()
	d.Components[0].Steps[0].To = 1 // a1 moves to B
	d.arrange()
	if got := stepIDsOf(d.Components[0]); !reflect.DeepEqual(got, []string{"a2"}) {
		t.Errorf("A steps = %v, want [a2]", got)
	}
	if got := stepIDsOf(d.Components[1]); !reflect.DeepEqual(got, []string{"b1", "a1"}) {
		t.Errorf("B steps = %v, want [b1 a1]", got)
	}
	if d.Components[1].Steps[1].To != 1 {
		t.Error("a moved step must report its new component")
	}
	// Dependencies and equipment links follow the step by id.
	if !reflect.DeepEqual(d.Components[0].Steps[0].Deps, []string{"a1"}) || !reflect.DeepEqual(d.Equipment[0].Steps, []string{"a1", "b1"}) {
		t.Errorf("a move must keep references: %v, %v", d.Components[0].Steps[0].Deps, d.Equipment[0].Steps)
	}
}

func TestRemovingAStepDropsReferencesToIt(t *testing.T) {
	d := twoComponents()
	if err := d.apply("rm-step:0.1"); err != nil { // a2
		t.Fatal(err)
	}
	if got := d.Components[1].Steps[0].Deps; len(got) != 0 {
		t.Errorf("b1 still depends on %v", got)
	}
	if err := d.apply("rm-step:0.0"); err != nil { // a1
		t.Fatal(err)
	}
	if got := d.Equipment[0].Steps; !reflect.DeepEqual(got, []string{"b1"}) {
		t.Errorf("equipment steps = %v, want [b1]", got)
	}
}

func TestNewStepsGetUniqueIDs(t *testing.T) {
	d := twoComponents()
	d.Components[0].Steps[0].ID = "s1"
	for i := 0; i < 3; i++ {
		if err := d.apply("add-step:1"); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	for _, c := range d.Components {
		for _, s := range c.Steps {
			if s.ID == "" || seen[s.ID] {
				t.Fatalf("step id %q is empty or repeated in %v", s.ID, d.Components)
			}
			seen[s.ID] = true
		}
	}
}

// A posted form that drops the step with id s1 and adds a step with no id: the
// new step takes the free id s1, and must not inherit the dependency and the
// equipment link that named the removed step.
func TestANewStepReusingARemovedIDInheritsNoLinks(t *testing.T) {
	d := parseForm(url.Values{
		"c.0.s.0.text": {"Preheat."}, "c.0.s.0.to": {"0"},
		"c.0.s.1.text": {"Bake."}, "c.0.s.1.to": {"0"}, "c.0.s.1.id": {"s2"}, "c.0.s.1.dep": {"s1"},
		"e.0.name": {"oven"}, "e.0.step": {"s1", "s2"},
	})
	d.arrange()
	steps := d.Components[0].Steps
	if steps[0].ID != "s1" {
		t.Fatalf("new step id = %q, want the free id s1", steps[0].ID)
	}
	if len(steps[1].Deps) != 0 {
		t.Errorf("Bake depends on %v, want nothing: the step it named is gone", steps[1].Deps)
	}
	if !reflect.DeepEqual(d.Equipment[0].Steps, []string{"s2"}) {
		t.Errorf("equipment steps = %v, want only s2", d.Equipment[0].Steps)
	}
}

func TestReorderingAndEdgeMoves(t *testing.T) {
	d := twoComponents()
	if err := d.apply("down-step:0.0"); err != nil {
		t.Fatal(err)
	}
	if got := stepIDsOf(d.Components[0]); !reflect.DeepEqual(got, []string{"a2", "a1"}) {
		t.Errorf("after down = %v", got)
	}
	if err := d.apply("up-step:0.0"); err != nil { // already first: stays
		t.Fatal(err)
	}
	if got := stepIDsOf(d.Components[0]); !reflect.DeepEqual(got, []string{"a2", "a1"}) {
		t.Errorf("up on the first step moved it: %v", got)
	}
	if err := d.apply("down-component:0"); err != nil {
		t.Fatal(err)
	}
	if d.Components[0].Name != "B" || d.Components[1].Name != "A" {
		t.Errorf("components = %q, %q after moving A down", d.Components[0].Name, d.Components[1].Name)
	}
}

func TestApplyRejectsWhatTheDraftDoesNotHave(t *testing.T) {
	for _, a := range []string{"rm-component:5", "add-line:2", "rm-line:0.0", "rm-qual:0.0.0", "up-step:0.9", "rm-equip:3", "nonsense", "rm-step:a.b", "add-step:0.1"} {
		d := twoComponents()
		if err := d.apply(a); err == nil {
			t.Errorf("apply(%q) = nil, want an error", a)
		}
	}
}

func TestParseFormClosesGapsAndKeepsOrder(t *testing.T) {
	f := url.Values{}
	f.Set("c.4.name", "later")
	f.Set("c.1.name", "earlier")
	f.Set("c.1.l.7.ing", "x")
	f.Set("c.1.l.2.ing", "y")
	d := parseForm(f)
	if len(d.Components) != 2 || d.Components[0].Name != "earlier" || d.Components[1].Name != "later" {
		t.Fatalf("components = %+v", d.Components)
	}
	if l := d.Components[0].Lines; len(l) != 2 || l[0].Ingredient != "y" || l[1].Ingredient != "x" {
		t.Errorf("lines = %+v", l)
	}
	if strings.Contains(d.Name, "c.") {
		t.Error("top-level fields starting with c must not be read as a component")
	}
}

func TestParseFormDoesNotConfuseCaloriesWithComponents(t *testing.T) {
	f := url.Values{}
	f.Set("calories", "200")
	f.Set("cook", "5 min")
	d := parseForm(f)
	if len(d.Components) != 0 || d.Calories != "200" || d.Cook != "5 min" {
		t.Errorf("draft = %+v", d)
	}
}
