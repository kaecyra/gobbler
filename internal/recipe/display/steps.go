package display

import (
	"fmt"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// FormatDuration renders "1 h 30 min"; a zero duration is "" (unknown).
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	mins := int((d + 30*time.Second) / time.Minute)
	h, m := mins/60, mins%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d h %d min", h, m)
	case h > 0:
		return fmt.Sprintf("%d h", h)
	case m > 0:
		return fmt.Sprintf("%d min", m)
	}
	return "under 1 min"
}

// StepNote is the duration and the passive marker, "20 min, passive".
func StepNote(s recipe.Step) string {
	var parts []string
	if d := FormatDuration(s.Duration); d != "" {
		parts = append(parts, d)
	}
	if s.Passive {
		parts = append(parts, "passive")
	}
	return strings.Join(parts, ", ")
}

// MeanwhileItem is one step offered while a passive step runs: its number in
// the reading order of the page, and its text.
type MeanwhileItem struct {
	Number int
	Text   string
}

// StepNumbers numbers every step of r by its place in the reading order of the
// page. It is the single source of step numbers: the ordered lists and the
// meanwhile items both use it, so they cannot drift apart.
func StepNumbers(r recipe.Recipe) map[recipe.StepID]int {
	number := map[recipe.StepID]int{}
	for i, ref := range r.LinearOrder() {
		number[ref.ID] = i + 1
	}
	return number
}

// BuildMeanwhile turns the derived guidance (recipe.Meanwhile) into display
// items keyed by the passive step they belong to. Steps without guidance have
// no entry. An invalid step graph is an error: it should have been rejected
// on save, so showing the page without guidance would hide a corrupt recipe.
func BuildMeanwhile(r recipe.Recipe, number map[recipe.StepID]int) (map[recipe.StepID][]MeanwhileItem, error) {
	guidance, err := r.Meanwhile()
	if err != nil {
		return nil, err
	}
	out := map[recipe.StepID][]MeanwhileItem{}
	for _, g := range guidance {
		for _, o := range g.Offered {
			text := r.Components[o.ComponentIndex].Steps[o.Position-1].Text
			out[g.Passive.ID] = append(out[g.Passive.ID], MeanwhileItem{Number: number[o.ID], Text: text})
		}
	}
	return out, nil
}
