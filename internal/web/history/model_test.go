package history

import (
	"context"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// TestBuildVersionNamesWhereTheTotalCameFrom covers each total source of
// ADR-00004: the version page must show which rule produced the total.
func TestBuildVersionNamesWhereTheTotalCameFrom(t *testing.T) {
	steps := []recipe.Component{{Steps: []recipe.Step{
		{ID: "a", Text: "A.", Duration: 10 * time.Minute},
		{ID: "b", Text: "B.", Duration: 20 * time.Minute, DependsOn: []recipe.StepID{"a"}},
	}}}
	tests := []struct {
		name     string
		doc      recipe.Recipe
		wantNote string
	}{
		{"stored", recipe.Recipe{Times: recipe.Times{Total: time.Hour}, Components: steps}, "as stated by the source"},
		{"critical path", recipe.Recipe{Components: steps}, "from the longest chain of steps"},
		{"sum", recipe.Recipe{Times: recipe.Times{Prep: 5 * time.Minute, Cook: 10 * time.Minute}}, "prep, cook and waiting times added"},
		{"unknown", recipe.Recipe{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := buildVersion(context.Background(), nil, 1, 1, 1, tt.doc)
			if err != nil {
				t.Fatalf("buildVersion: %v", err)
			}
			if v.TotalNote != tt.wantNote {
				t.Errorf("TotalNote = %q, want %q", v.TotalNote, tt.wantNote)
			}
		})
	}
}
