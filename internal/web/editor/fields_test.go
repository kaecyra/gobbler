package editor

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// postFields saves the stored recipe with the ADR-00022 fields set: the meal
// type and spice level of the recipe, and the stage kind and title of its
// first step.
func (e *env) postFields(meal, spice, kind, title string) result {
	e.t.Helper()
	f := e.form()
	f.Set("meal_type", meal)
	f.Set("spice", spice)
	f.Set(sField(0, 0, "kind"), kind)
	f.Set(sField(0, 0, "title"), title)
	return e.post(f)
}

func firstStep(r recipe.Recipe) recipe.Step { return r.Components[0].Steps[0] }

func TestTheEditorSavesMealTypeSpiceLevelStageKindAndTitle(t *testing.T) {
	e := newEnv(t)
	if res := e.postFields("dessert", "2", "bake", "Bake it"); res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", res.Code, res.body())
	}
	got, _ := e.stored()
	if got.MealType != recipe.MealDessert || got.SpiceLevel == nil || *got.SpiceLevel != 2 {
		t.Errorf("stored meal %q spice %v, want dessert and 2", got.MealType, got.SpiceLevel)
	}
	if s := firstStep(got); s.Kind != recipe.StageBake || s.Title != "Bake it" {
		t.Errorf("stored step kind %q title %q, want bake and %q", s.Kind, s.Title, "Bake it")
	}

	// The page shows them back, selected.
	body := e.get().body()
	for _, want := range []string{
		`<option value="dessert" selected`, `<option value="2" selected`,
		`<option value="bake" selected`, `value="Bake it"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor page lacks %s", want)
		}
	}
}

func TestSpiceZeroIsNotUnsetAndEmptyClearsEverything(t *testing.T) {
	e := newEnv(t)
	if res := e.postFields("", "0", "", ""); res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", res.Code, res.body())
	}
	if got, _ := e.stored(); got.SpiceLevel == nil || *got.SpiceLevel != 0 {
		t.Errorf("stored spice = %v, want 0 (not spicy)", got.SpiceLevel)
	}
	if res := e.postFields("", "", "", ""); res.Code != http.StatusSeeOther {
		t.Fatalf("clearing status = %d: %s", res.Code, res.body())
	}
	got, _ := e.stored()
	if got.SpiceLevel != nil || got.MealType != "" || firstStep(got).Kind != "" || firstStep(got).Title != "" {
		t.Errorf("after clearing: meal %q spice %v step %+v, want all unset", got.MealType, got.SpiceLevel, firstStep(got))
	}
}

func TestValuesOutsideTheClosedSetsAreRefusedWithAMessageAndSaveNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		meal, spice, kind, msg string
	}{
		"meal type":  {"brunch", "", "", `is not a meal type`},
		"spice":      {"", "4", "", `is not a spice level`},
		"spice text": {"", "hot", "", `is not a spice level`},
		"stage kind": {"", "", "fry", `is not a stage kind`},
	} {
		e := newEnv(t)
		_, before := e.stored()
		res := e.postFields(tc.meal, tc.spice, tc.kind, "")
		if res.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, res.Code)
			continue
		}
		if !strings.Contains(res.body(), tc.msg) {
			t.Errorf("%s: no message %q in the page", name, tc.msg)
		}
		if _, v := e.stored(); v != before {
			t.Errorf("%s: version %d -> %d; a refused save must change nothing", name, before, v)
		}
	}
}

func TestParseFormReadsTheNewFields(t *testing.T) {
	f := url.Values{
		"meal_type": {"side"}, "spice": {"1"},
		"c.0.s.0.kind": {"rest"}, "c.0.s.0.title": {"Rest"},
	}
	d := parseForm(f)
	if d.MealType != "side" || d.Spice != "1" {
		t.Errorf("meal %q spice %q, want side and 1", d.MealType, d.Spice)
	}
	if s := d.Components[0].Steps[0]; s.Kind != "rest" || s.Title != "Rest" {
		t.Errorf("step kind %q title %q, want rest and Rest", s.Kind, s.Title)
	}
}
