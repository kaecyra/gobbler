package display

import (
	"fmt"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// TimeRow is one labelled time of the document view.
type TimeRow struct {
	Label string
	Text  string
	// Scales is true for times that may change when the recipe is scaled (cook
	// and special times; prep does not, ADR-00005). Callers decide whether to
	// show a hint.
	Scales bool
}

// Times is the time section of the document view: the set times in order, the
// derived total and where it came from (ADR-00004: "the UI shows which").
type Times struct {
	// Rows are prep, cook and the special times that are set, in that order.
	Rows  []TimeRow
	Total string
	// TotalNote names the rule that produced the total; empty when unknown.
	TotalNote string
}

// BuildTimes builds the time section of r. It returns the step graph error
// when the graph is invalid and no stored total exists.
func BuildTimes(r recipe.Recipe) (Times, error) {
	var t Times
	row := func(label string, text string, scales bool) {
		if text != "" {
			t.Rows = append(t.Rows, TimeRow{Label: label, Text: text, Scales: scales})
		}
	}
	row("Prep", FormatDuration(r.Times.Prep), false)
	row("Cook", FormatDuration(r.Times.Cook), true)
	for _, s := range r.Times.Special {
		row(s.Label, FormatDuration(s.Duration), true)
	}
	total, err := r.TotalTime()
	if err != nil {
		return Times{}, fmt.Errorf("total time: %w", err)
	}
	t.Total = FormatDuration(total.Duration)
	switch total.Source {
	case recipe.TotalStored:
		t.TotalNote = "as stated by the source"
	case recipe.TotalCriticalPath:
		t.TotalNote = "from the longest chain of steps"
	case recipe.TotalSum:
		t.TotalNote = "prep, cook and waiting times added"
	}
	return t, nil
}
