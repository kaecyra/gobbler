package editor

import (
	"bytes"
	"strings"
	"testing"

	ingestcore "github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func fieldsBase() ingestcore.Draft {
	d := graphBase()
	spice := 1
	d.MealType, d.SpiceLevel = recipe.MealDessert, &spice
	d.Components[0].Steps[1].Kind, d.Components[0].Steps[1].Title = recipe.StageBake, "Bake it"
	return d
}

// A draft under review shows the ADR-00022 fields as the rules read them.
func TestReviewShowsTheRecipeFieldsOfADraft(t *testing.T) {
	e := newEnv(t)
	form, err := NewUnsaved(e.cat).Open(e.ctx, fieldsBase(), ReviewOptions{Action: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := form.Component().Render(e.ctx, &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{`name="meal_type"`, `<option value="dessert" selected`, `name="spice"`, `<option value="1" selected`, `name="c.0.s.1.kind"`, `<option value="bake" selected`, `value="Bake it"`} {
		if !strings.Contains(body, want) {
			t.Errorf("review form lacks %q", want)
		}
	}
}

// What the user sets or clears on the review screen goes back into the draft.
func TestReviewFieldsRoundTripAndCanBeCleared(t *testing.T) {
	e := newEnv(t)
	u := NewUnsaved(e.cat)
	v := postedGraphForm(t, e, u, fieldsBase())
	v.Set("meal_type", "main")
	v.Set("spice", "0")
	v.Set("c.0.s.0.kind", "prep")
	v.Set("c.0.s.0.title", " Start ")
	v.Set("c.0.s.1.kind", "")
	v.Set("c.0.s.1.title", "")
	out, err := u.Submit(e.ctx, v, fieldsBase(), ReviewOptions{Action: "/x"})
	if err != nil || !out.Ready {
		t.Fatalf("Submit: ready=%v err=%v", out.Ready, err)
	}
	d := out.Draft
	steps := d.Components[0].Steps
	if d.MealType != recipe.MealMain || d.SpiceLevel == nil || *d.SpiceLevel != 0 {
		t.Errorf("meal type %q spice %v, want main and 0", d.MealType, d.SpiceLevel)
	}
	if steps[0].Kind != recipe.StagePrep || steps[0].Title != "Start" || steps[1].Kind != "" || steps[1].Title != "" {
		t.Errorf("steps = %+v, want prep and a title on the first, both cleared on the second", steps)
	}

	v.Set("meal_type", "")
	v.Set("spice", "")
	out, err = u.Submit(e.ctx, v, fieldsBase(), ReviewOptions{Action: "/x"})
	if err != nil || !out.Ready {
		t.Fatalf("Submit: ready=%v err=%v", out.Ready, err)
	}
	if out.Draft.MealType != "" || out.Draft.SpiceLevel != nil {
		t.Errorf("meal type %q spice %v, want both cleared", out.Draft.MealType, out.Draft.SpiceLevel)
	}
}
