package recipeview

import (
	"fmt"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// meanwhileItem is one step offered while a passive step runs: its number in
// the reading order of the page, and its text.
type meanwhileItem struct {
	Number int
	Text   string
}

// buildMeanwhile turns the derived guidance (recipe.Meanwhile) into display
// items keyed by the passive step they belong to. Steps without guidance have
// no entry. An invalid step graph is an error: it should have been rejected
// on save, so showing the page without guidance would hide a corrupt recipe.
func buildMeanwhile(r recipe.Recipe) (map[recipe.StepID][]meanwhileItem, error) {
	guidance, err := r.Meanwhile()
	if err != nil {
		return nil, fmt.Errorf("meanwhile guidance: %w", err)
	}
	number := map[recipe.StepID]int{}
	for i, ref := range r.LinearOrder() {
		number[ref.ID] = i + 1
	}
	out := map[recipe.StepID][]meanwhileItem{}
	for _, g := range guidance {
		for _, o := range g.Offered {
			text := r.Components[o.ComponentIndex].Steps[o.Position-1].Text
			out[g.Passive.ID] = append(out[g.Passive.ID], meanwhileItem{Number: number[o.ID], Text: text})
		}
	}
	return out, nil
}
