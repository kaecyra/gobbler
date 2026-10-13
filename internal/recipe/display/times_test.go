package display

import (
	"reflect"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
)

func TestBuildTimes(t *testing.T) {
	steps := []recipe.Component{{Steps: []recipe.Step{
		{ID: "a", Text: "A.", Duration: 10 * time.Minute},
		{ID: "b", Text: "B.", Duration: 20 * time.Minute, DependsOn: []recipe.StepID{"a"}},
	}}}
	tests := []struct {
		name      string
		r         recipe.Recipe
		wantRows  []TimeRow
		wantTotal string
		wantNote  string
	}{
		{"stored", recipe.Recipe{Times: recipe.Times{Total: time.Hour}, Components: steps}, nil, "1 h", "as stated by the source"},
		{"critical path", recipe.Recipe{Components: steps}, nil, "30 min", "from the longest chain of steps"},
		{
			"sum",
			recipe.Recipe{Times: recipe.Times{Prep: 5 * time.Minute, Cook: 10 * time.Minute, Special: []recipe.SpecialTime{
				{Label: recipe.LabelRest, Duration: 15 * time.Minute}, {Label: recipe.LabelChill},
			}}},
			[]TimeRow{
				{Label: "Prep", Text: "5 min"},
				{Label: "Cook", Text: "10 min", Scales: true},
				{Label: "rest", Text: "15 min", Scales: true},
			},
			"30 min", "prep, cook and waiting times added",
		},
		{"unknown", recipe.Recipe{}, nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildTimes(tt.r)
			if err != nil {
				t.Fatalf("BuildTimes: %v", err)
			}
			if !reflect.DeepEqual(got.Rows, tt.wantRows) {
				t.Errorf("Rows = %+v, want %+v", got.Rows, tt.wantRows)
			}
			if got.Total != tt.wantTotal || got.TotalNote != tt.wantNote {
				t.Errorf("Total, note = %q, %q; want %q, %q", got.Total, got.TotalNote, tt.wantTotal, tt.wantNote)
			}
		})
	}
}

func TestBuildTimesInvalidGraph(t *testing.T) {
	r := recipe.Recipe{Components: []recipe.Component{{Steps: []recipe.Step{
		{ID: "a", Text: "A.", DependsOn: []recipe.StepID{"missing"}},
	}}}}
	if _, err := BuildTimes(r); err == nil {
		t.Fatal("BuildTimes accepted an invalid step graph with no stored total")
	}
}
