package editor

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// Steps are numbered across the whole recipe, as the recipe page and cook
// mode number them.
func TestStepNumCountsAcrossComponents(t *testing.T) {
	v := view{D: draft{Components: []compDraft{
		{Steps: []stepDraft{{ID: "a"}, {ID: "b"}}},
		{},
		{Steps: []stepDraft{{ID: "c"}, {ID: "d"}, {ID: "e"}}},
	}}}
	for _, tc := range []struct{ ci, si, want int }{{0, 0, 1}, {0, 1, 2}, {2, 0, 3}, {2, 2, 5}} {
		if got := v.stepNum(tc.ci, tc.si); got != tc.want {
			t.Errorf("stepNum(%d, %d) = %d, want %d", tc.ci, tc.si, got, tc.want)
		}
	}
}

func TestAfterNamesTheStepsAStepWaitsFor(t *testing.T) {
	v := view{D: twoComponents()}
	for _, tc := range []struct {
		name string
		deps []string
		want string
	}{
		{"none", nil, ""},
		{"one", []string{"a2"}, "after 2"},
		{"several, sorted by number", []string{"b1", "a1"}, "after 1, 3"},
		{"an id the form does not show is left out", []string{"gone", "a1"}, "after 1"},
		{"only unknown ids", []string{"gone"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := v.after(stepDraft{Deps: tc.deps}); got != tc.want {
				t.Errorf("after(%v) = %q, want %q", tc.deps, got, tc.want)
			}
		})
	}
}

func TestNeedsLookCountsLowItemsAndPointsAtTheFirstShown(t *testing.T) {
	low := mark{Low: true}
	for _, tc := range []struct {
		name  string
		d     draft
		n     int
		first string
	}{
		{"nothing flagged", twoComponents(), 0, ""},
		{"an LLM mark is not a look", draft{Marks: map[string]mark{"name": {LLM: true}}}, 0, ""},
		{
			"a metadata field comes before the lines",
			draft{
				Marks:      map[string]mark{"cook": low},
				Components: []compDraft{{Lines: []lineDraft{{}, {Mark: low}}}},
			},
			2, "f-cook",
		},
		{
			"lines come before steps, steps before equipment",
			draft{
				Components: []compDraft{
					{Steps: []stepDraft{{ID: "s", Mark: low}}},
					{Lines: []lineDraft{{Mark: low}}},
				},
				Equipment: []equipDraft{{Mark: low}},
			},
			3, "line-1-0",
		},
		{"a step alone", draft{Components: []compDraft{{Steps: []stepDraft{{ID: "a"}, {ID: "b", Mark: low}}}}}, 1, "step-0-1"},
		{"equipment alone", draft{Equipment: []equipDraft{{}, {Mark: low}}}, 1, "equip-1"},
		{"the author, last of the metadata", draft{Marks: map[string]mark{"author": low}}, 1, "f-author"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, first := view{D: tc.d}.needsLook()
			if n != tc.n || first != tc.first {
				t.Errorf("needsLook() = %d, %q; want %d, %q", n, first, tc.n, tc.first)
			}
		})
	}
}

func TestDisclosuresOpenWhenThereIsSomethingToSeeOrFillIn(t *testing.T) {
	lines := []struct {
		name string
		l    lineDraft
		want bool
	}{
		{"a read line", lineDraft{Ingredient: "flour", Raw: "2 cups flour"}, false},
		{"low confidence", lineDraft{Ingredient: "flour", Mark: mark{Low: true}}, true},
		{"an LLM mark alone", lineDraft{Ingredient: "flour", Mark: mark{LLM: true}}, false},
		{"an error", lineDraft{Ingredient: "flour", Err: "bad"}, true},
		{"a catalog question", lineDraft{Ingredient: "zorb", OfferProposal: true}, true},
		{"proposed", lineDraft{Ingredient: "zorb", Propose: true}, true},
		{"just added", lineDraft{}, true},
		{"free text", lineDraft{Raw: "salt to taste"}, false},
		{"an empty qualifier", lineDraft{Ingredient: "flour", Quals: []qualDraft{{Text: "sifted"}, {Text: " "}}}, true},
		{"filled qualifiers", lineDraft{Ingredient: "flour", Quals: []qualDraft{{Text: "sifted"}}}, false},
	}
	for _, tc := range lines {
		if got := tc.l.open(); got != tc.want {
			t.Errorf("line %s: open() = %t, want %t", tc.name, got, tc.want)
		}
	}
	steps := []struct {
		name string
		s    stepDraft
		want bool
	}{
		{"a written step", stepDraft{Text: "Mix."}, false},
		{"just added", stepDraft{}, true},
		{"low confidence", stepDraft{Text: "Mix.", Mark: mark{Low: true}}, true},
		{"an error", stepDraft{Text: "Mix.", Err: "bad"}, true},
	}
	for _, tc := range steps {
		if got := tc.s.open(); got != tc.want {
			t.Errorf("step %s: open() = %t, want %t", tc.name, got, tc.want)
		}
	}
	equipment := []struct {
		name string
		e    equipDraft
		want bool
	}{
		{"named", equipDraft{Name: "whisk"}, false},
		{"just added", equipDraft{}, true},
		{"a catalog question", equipDraft{Name: "zorb", OfferProposal: true}, true},
		{"low confidence", equipDraft{Name: "whisk", Mark: mark{Low: true}}, true},
	}
	for _, tc := range equipment {
		if got := tc.e.open(); got != tc.want {
			t.Errorf("equipment %s: open() = %t, want %t", tc.name, got, tc.want)
		}
	}
}

// The step chooser lists the step itself in its place, disabled and without
// a name, so it can never be posted as its own dependency.
func TestStepChooserShowsTheStepItselfDisabled(t *testing.T) {
	v := view{D: twoComponents()}
	var b strings.Builder
	if err := stepChooser(v, "Starts after", "", "c.0.s.1.dep", "a2", []string{"a1"}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	self, _, _ := strings.Cut(out[strings.Index(out, `class="dep self"`):], "</label>")
	if !strings.Contains(self, "This step") || !strings.Contains(self, "disabled") || strings.Contains(self, "name=") {
		t.Errorf("the step itself is not a disabled, unnamed option: %s", self)
	}
	if strings.Contains(out, `value="a2"`) {
		t.Error("the step is offered as its own dependency")
	}
	for _, want := range []string{`name="c.0.s.1.dep" value="a1" checked`, `name="c.0.s.1.dep" value="b1"`, ">A<", ">B<"} {
		if !strings.Contains(out, want) {
			t.Errorf("chooser lacks %s", want)
		}
	}
	if strings.Count(out, `class="dep"`) != 2 {
		t.Errorf("want the two other steps offered: %s", out)
	}
}

func TestLineNamesAreUniqueAcrossComponents(t *testing.T) {
	one := view{D: draft{Components: []compDraft{{}}}}
	if got := one.lineRef(0, 2); got != "line 3" {
		t.Errorf("one component: %q", got)
	}
	two := view{D: draft{Components: []compDraft{{Name: "Dough"}, {}}}}
	if a, b := two.lineRef(0, 0), two.lineRef(1, 0); a != "Dough line 1" || b != "Part 2 line 1" {
		t.Errorf("two components: %q, %q", a, b)
	}
}

// A drag posts "move-...:<position>.<from>.<to>"; the item moves there and the
// rest shift.
func TestMoveActionsReorderWithinAList(t *testing.T) {
	lines := func(names ...string) []lineDraft {
		var out []lineDraft
		for _, n := range names {
			out = append(out, lineDraft{Ingredient: n})
		}
		return out
	}
	ings := func(c compDraft) []string {
		var out []string
		for _, l := range c.Lines {
			out = append(out, l.Ingredient)
		}
		return out
	}
	d := draft{Components: []compDraft{{Name: "A", Lines: lines("a", "b", "c", "d")}, {Name: "B"}}}
	if err := d.apply("move-line:0.0.2"); err != nil {
		t.Fatal(err)
	}
	if got := ings(d.Components[0]); !reflect.DeepEqual(got, []string{"b", "c", "a", "d"}) {
		t.Errorf("move down: %v", got)
	}
	if err := d.apply("move-line:0.3.0"); err != nil {
		t.Fatal(err)
	}
	if got := ings(d.Components[0]); !reflect.DeepEqual(got, []string{"d", "b", "c", "a"}) {
		t.Errorf("move up: %v", got)
	}

	d.Components[0].Lines[0].Quals = []qualDraft{{Text: "x"}, {Text: "y"}, {Text: "z"}}
	if err := d.apply("move-qual:0.0.2.0"); err != nil {
		t.Fatal(err)
	}
	if q := d.Components[0].Lines[0].Quals; q[0].Text != "z" || q[1].Text != "x" || q[2].Text != "y" {
		t.Errorf("qualifiers: %v", q)
	}

	if err := d.apply("move-component:1.0"); err != nil {
		t.Fatal(err)
	}
	if d.Components[0].Name != "B" || d.Components[1].Name != "A" {
		t.Errorf("components: %q, %q", d.Components[0].Name, d.Components[1].Name)
	}

	s := twoComponents()
	if err := s.apply("move-step:0.1.0"); err != nil {
		t.Fatal(err)
	}
	if got := stepIDsOf(s.Components[0]); !reflect.DeepEqual(got, []string{"a2", "a1"}) {
		t.Errorf("steps: %v", got)
	}
	s.Equipment = append(s.Equipment, equipDraft{Name: "bowl"}, equipDraft{Name: "pan"})
	if err := s.apply("move-equip:2.0"); err != nil {
		t.Fatal(err)
	}
	if s.Equipment[0].Name != "pan" || s.Equipment[1].Name != "whisk" || s.Equipment[2].Name != "bowl" {
		t.Errorf("equipment: %+v", s.Equipment)
	}
}

func TestMoveActionsOutOfRangeAreRefused(t *testing.T) {
	for _, action := range []string{
		"move-line:0.0.4", "move-line:0.4.0", "move-line:0.1", "move-line:2.0.0",
		"move-qual:0.0.0.1", "move-step:0.0.2", "move-equip:0.3", "move-equip:0", "move-component:0.2",
	} {
		d := twoComponents()
		d.Components[0].Lines = []lineDraft{{Ingredient: "a"}, {Ingredient: "b"}}
		if err := d.apply(action); err == nil {
			t.Errorf("%s was accepted", action)
		}
	}
}

// An Add button says which new control to focus when the form comes back.
func TestAddActionsFocusTheNewControl(t *testing.T) {
	for _, tc := range []struct{ action, want string }{
		{"add-component", "c.2.name"},
		{"add-line:1", "c.1.l.0.qty"},
		{"add-step:0", "c.0.s.2.text"},
		{"add-equip", "e.1.name"},
		{"add-qual:0.0", "c.0.l.0.q.1.text"},
		{"rm-step:0.0", ""},
	} {
		d := twoComponents()
		d.Components[0].Lines = []lineDraft{{Ingredient: "a", Quals: []qualDraft{{Text: "x"}}}}
		if err := d.apply(tc.action); err != nil {
			t.Fatal(err)
		}
		if d.Focus != tc.want {
			t.Errorf("%s: Focus = %q, want %q", tc.action, d.Focus, tc.want)
		}
	}
}

// The parts panel credits the source's headings only while the draft still
// has one part for each of them.
func TestPartsTextSaysWhereThePartsCameFrom(t *testing.T) {
	three := []compDraft{{}, {}, {}}
	for _, tc := range []struct {
		name   string
		v      view
		prefix string
	}{
		{"one part", view{D: draft{Components: []compDraft{{}}}}, "This recipe is one part."},
		{"a stored recipe", view{D: draft{Components: three}}, "This recipe has three parts."},
		{"a draft as the page had it", view{D: draft{Components: three}, Review: &ReviewOptions{SourceParts: 3}}, "The page lists its ingredients under three headings, so the recipe is read as three parts."},
		{"a draft with a part added", view{D: draft{Components: three}, Review: &ReviewOptions{SourceParts: 2}}, "This recipe has three parts."},
		{"past the words", view{D: draft{Components: make([]compDraft, 12)}}, "This recipe has 12 parts."},
	} {
		if got := tc.v.partsText(); !strings.HasPrefix(got, tc.prefix) {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
	if got := partCounts(compDraft{Lines: []lineDraft{{}}, Steps: []stepDraft{{}, {}}}); got != "1 ingredient · 2 steps" {
		t.Errorf("partCounts = %q", got)
	}
	if partTitle(compDraft{Name: "  "}) != "Unnamed part" || partTitle(compDraft{Name: "Glaze"}) != "Glaze" {
		t.Error("partTitle")
	}
}
