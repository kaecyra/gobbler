package history

import (
	"context"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// TestBuildVersionShowsTimesAndTotalNote is a wiring check: the version view
// carries the rows, total and total-source note from display.BuildTimes. The
// per-TotalSource cases live in that package's tests.
func TestBuildVersionShowsTimesAndTotalNote(t *testing.T) {
	doc := recipe.Recipe{Times: recipe.Times{Prep: 5 * time.Minute, Cook: 10 * time.Minute}}
	v, err := buildVersion(context.Background(), nil, 1, 1, 1, doc)
	if err != nil {
		t.Fatalf("buildVersion: %v", err)
	}
	if len(v.Times) != 2 || v.Times[0].Label != "Prep" || v.Times[1].Text != "10 min" {
		t.Errorf("Times = %+v, want Prep and Cook rows", v.Times)
	}
	if v.Total != "15 min" || v.TotalNote != "prep, cook and waiting times added" {
		t.Errorf("Total, note = %q, %q", v.Total, v.TotalNote)
	}
}
