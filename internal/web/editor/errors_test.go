package editor

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// The first error is the one the callout and the save bar link to, so it is
// the first the form shows: metadata, then ingredient lines, steps, notes and
// equipment, whatever order the errors were found in.
func TestErrorsCountsAndPointsAtTheFirstInDOMOrder(t *testing.T) {
	two := func(c compDraft) []compDraft { return []compDraft{{}, c} }
	for _, tc := range []struct {
		name      string
		d         draft
		wantN     int
		wantFirst string
	}{
		{"clean", draft{Errs: map[string]string{}}, 0, ""},
		{"nil map", draft{}, 0, ""},
		{"a problem about the whole recipe is not a field", draft{Problems: []string{"cycle"}}, 0, ""},
		{"one metadata field", draft{Errs: map[string]string{"video": "no"}}, 1, "err-video"},
		{"metadata in form order, not map order", draft{Errs: map[string]string{"video": "x", "prep": "x", "name": "x", "spice": "x"}}, 4, "err-name"},
		{"line before step before note before equipment", draft{
			Components: two(compDraft{Lines: []lineDraft{{}, {Err: "x"}}, Steps: []stepDraft{{Err: "x"}}}),
			Notes:      []noteDraft{{Err: "x"}},
			Equipment:  []equipDraft{{Err: "x"}},
		}, 4, "line-1-1"},
		{"a line anywhere beats a step in an earlier part", draft{
			Components: []compDraft{{Steps: []stepDraft{{Err: "x"}}}, {Lines: []lineDraft{{Err: "x"}}}},
		}, 2, "line-1-0"},
		{"step before note", draft{
			Components: []compDraft{{Steps: []stepDraft{{}, {Err: "x"}}}},
			Notes:      []noteDraft{{Err: "x"}},
		}, 2, "step-0-1"},
		{"metadata beats rows", draft{
			Errs:  map[string]string{"calories": "x"},
			Notes: []noteDraft{{Err: "x"}},
		}, 2, "err-calories"},
		{"note then equipment", draft{Notes: []noteDraft{{}, {Err: "x"}}, Equipment: []equipDraft{{Err: "x"}}}, 2, "note-1"},
		{"equipment alone", draft{Equipment: []equipDraft{{}, {Err: "x"}}}, 1, "equip-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, first := view{D: tc.d}.errors()
			if n != tc.wantN || first != tc.wantFirst {
				t.Errorf("errors() = (%d, %q), want (%d, %q)", n, first, tc.wantN, tc.wantFirst)
			}
		})
	}
}

func TestErrorTextsAreSingularAndPlural(t *testing.T) {
	for _, tc := range []struct {
		n                int
		notSaved, cantSv string
	}{
		{1, "1 field needs fixing", "1 field stops this being saved"},
		{2, "2 fields need fixing", "2 fields stop this being saved"},
		{11, "11 fields need fixing", "11 fields stop this being saved"},
	} {
		if got := notSavedText(tc.n); got != tc.notSaved {
			t.Errorf("notSavedText(%d) = %q, want %q", tc.n, got, tc.notSaved)
		}
		if got := cantSaveText(tc.n); got != tc.cantSv {
			t.Errorf("cantSaveText(%d) = %q, want %q", tc.n, got, tc.cantSv)
		}
	}
}

// A form without errors shows neither the callout nor the bar's error line; one
// with errors shows both.
func TestRenderShowsTheCalloutAndBarLineOnlyWithErrors(t *testing.T) {
	render := func(d draft) string {
		var b strings.Builder
		if err := editorForm(view{D: d}).Render(context.Background(), &b); err != nil {
			t.Fatalf("render: %v", err)
		}
		return b.String()
	}
	clean := render(draft{Errs: map[string]string{}, Problems: []string{"a cycle"}})
	for _, marker := range []string{`data-testid="not-saved"`, `data-testid="bar-errors"`} {
		if strings.Contains(clean, marker) {
			t.Errorf("form without field errors renders %s", marker)
		}
	}
	bad := render(draft{Errs: map[string]string{"video": "not a video"}})
	for _, marker := range []string{`data-testid="not-saved"`, `data-testid="bar-errors"`, `href="#err-video"`, `id="err-video"`} {
		if !strings.Contains(bad, marker) {
			t.Errorf("form with a field error lacks %s", marker)
		}
	}
}

// Every key build writes into Errs must be one errors() looks for, or that
// error is shown inline but missing from the callout's count and link.
func TestBuildErrorKeysAreAllCounted(t *testing.T) {
	e := newEnv(t)
	h := &handler{Deps: Deps{Recipes: e.svc, Catalog: e.cat}}
	d := fromRecipe(e.fixture(), e.names())
	d.Name = ""
	d.YieldQty, d.Servings, d.Calories = "x", "x", "x"
	d.Prep, d.Cook, d.Total = "x", "x", "x"
	d.MealType, d.Cuisine, d.Spice, d.Video = "x", "x", "x", "x"
	if _, _, _, ok, err := h.build(e.ctx, &d, recipe.Recipe{}); err != nil || ok {
		t.Fatalf("build ok=%v err=%v, want a refusal", ok, err)
	}
	var keys []string
	for k := range d.Errs {
		keys = append(keys, k)
	}
	if len(keys) != len(errFieldOrder) {
		t.Fatalf("build wrote %d error keys %v, errFieldOrder has %d %v", len(keys), keys, len(errFieldOrder), errFieldOrder)
	}
	if n, _ := (view{D: d}).errors(); n != len(d.Errs) {
		t.Errorf("errors() counts %d, build refused %d fields: %v", n, len(d.Errs), reflect.ValueOf(d.Errs).MapKeys())
	}
}
