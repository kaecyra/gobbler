package ingest

import (
	"strings"
	"testing"
)

// The parser keeps no unit table: every unit spelling is answered by the
// migrated catalog, including the spellings migration 00003 added.
func TestParserUnitsAreTheCatalogs(t *testing.T) {
	e := newEnv(t)
	text := "Unit Soup\n\nIngredients\n1 c sugar\n100 gr flour\n1 bag spinach\n2 boxes pasta\n2 pieces garlic\n3 zorbs butter\n\nDirections\n1. Mix.\n"
	d, err := e.pipeline(t, nil).FromText(e.ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	lines := d.Components[0].Lines
	for i, want := range []string{"cup", "g", "bag", "box", "each", ""} {
		if got := lines[i].Amount.Unit.Name; got != want {
			t.Errorf("%q: unit = %q, want %q", lines[i].Raw, got, want)
		}
	}
	if z := lines[5]; z.Name != "zorbs butter" || !strings.Contains(strings.Join(z.Issues, "; "), "unrecognised unit") {
		t.Errorf("unknown unit line = %q issues %v, want the word kept in the name and flagged", z.Name, z.Issues)
	}
}

// An unknown unit on a line whose ingredient is also unknown: the unit word
// stays in the name, so the whole name is proposed as a new ingredient. The
// line is below full confidence and flagged as an unmatched ingredient; the
// parser raises "unrecognised unit" only when the rest of the name matches.
func TestUnknownUnitOnUnmatchedIngredientIsFlaggedAndProposed(t *testing.T) {
	e := newEnv(t)
	d, err := e.pipeline(t, nil).FromText(e.ctx, "Dragon\n\nIngredients\n3 zorbs dragonfruit\n\nDirections\n1. Mix.\n")
	if err != nil {
		t.Fatal(err)
	}
	l := d.Components[0].Lines[0]
	if l.Name != "zorbs dragonfruit" || l.Ingredient != 0 || !l.Amount.Unit.IsZero() {
		t.Errorf("line = name %q ingredient %d unit %q, want the unit word kept in an unmatched name", l.Name, l.Ingredient, l.Amount.Unit.Name)
	}
	if l.Confidence >= 1 {
		t.Errorf("confidence = %v, want below 1", l.Confidence)
	}
	if got := strings.Join(l.Issues, "; "); !strings.Contains(got, `ingredient "zorbs dragonfruit" not matched`) {
		t.Errorf("issues = %q, want the ingredient flagged as unmatched", got)
	}
	if len(d.Proposed) != 1 || d.Proposed[0].Name != "zorbs dragonfruit" {
		t.Errorf("proposed = %+v, want the whole name proposed", d.Proposed)
	}
}
