package ingest

import (
	"slices"
	"strings"
	"testing"
	"time"
)

const metaRecipeTail = "\n\nIngredients\n2 cups flour\n\nDirections\n1. Mix.\n"

// Labelled lines in pasted text set the draft's times, yield and calories at
// full confidence, the same as structured data does, and are not left in the
// description.
func TestFromTextReadsMetaLines(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx,
		"Banana Bread\nPrep time: 15 minutes\nCook time: 1 hour\nTotal time: 1 hr 15 min\nMakes 1 loaf\nCalories: 280 kcal"+metaRecipeTail)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		got  Field[time.Duration]
		want time.Duration
	}{{"prep", d.Prep, 15 * time.Minute}, {"cook", d.Cook, time.Hour}, {"total", d.Total, 75 * time.Minute}} {
		if c.got.Value != c.want || c.got.Confidence != 1 {
			t.Errorf("%s = %+v, want %v at confidence 1", c.name, c.got, c.want)
		}
	}
	if y := d.Yield.Value; y.Unit != "loaf" || y.Qty.String() != "1" || d.Yield.Confidence != 1 {
		t.Errorf("yield = %+v, want 1 loaf at confidence 1", d.Yield)
	}
	if d.Calories.Value != 280 || d.Calories.Confidence != 1 {
		t.Errorf("calories = %+v, want 280 at confidence 1", d.Calories)
	}
	if strings.Contains(d.Description.Value, "Prep") || strings.Contains(d.Description.Value, "Calories") {
		t.Errorf("description = %q, want the meta lines gone", d.Description.Value)
	}
}

func TestFromTextServesSetsServings(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, "Soup\nServes 4"+metaRecipeTail)
	if err != nil {
		t.Fatal(err)
	}
	if d.Servings.Value.String() != "4" || d.Yield.Value.Unit != "servings" {
		t.Errorf("servings = %v, yield = %+v, want 4 servings", d.Servings.Value, d.Yield.Value)
	}
}

// A stated value the rules cannot read is flagged, not guessed, and its text
// stays visible in the description.
func TestFromTextFlagsUnreadableMeta(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, "Soup\nPrep time: a while\nYield: plenty\nCalories: lots"+metaRecipeTail)
	if err != nil {
		t.Fatal(err)
	}
	if d.Prep.Value != 0 || d.Yield.Value.Unit != "" || d.Calories.Value != 0 {
		t.Errorf("prep %v yield %+v calories %v, want none guessed", d.Prep.Value, d.Yield.Value, d.Calories.Value)
	}
	for _, is := range []string{"unparsed_duration", IssueUnreadableYield, "unreadable_calories"} {
		if !slices.Contains(d.Issues, is) {
			t.Errorf("issues = %v, missing %q", d.Issues, is)
		}
	}
	if !strings.Contains(d.Description.Value, "Prep time: a while") {
		t.Errorf("description = %q, want the unread line kept", d.Description.Value)
	}
}
