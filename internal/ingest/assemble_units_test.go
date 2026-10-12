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
