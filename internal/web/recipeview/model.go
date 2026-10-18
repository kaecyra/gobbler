package recipeview

import (
	"context"
	"fmt"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/ribbon"
)

// hintTime is shown on cook and special times of a scaled view (ADR-00005):
// times are not scaled, so the page says they may need attention.
const hintTime = "time may change"

// hintLargerPan is shown on sized equipment of a view scaled up (ADR-00005,
// ADR-00019 as amended): equipment is not scaled, so the page says the pan may not hold
// the new amount.
const hintLargerPan = "may need a larger pan"

// equipmentHint is the hint beside one equipment line: the larger-pan hint on
// sized catalog equipment of a view scaled up, and nothing otherwise. A view
// scaled down needs a smaller pan at most, never a larger one.
func equipmentHint(scaledUp, sized bool) string {
	if scaledUp && sized {
		return hintLargerPan
	}
	return ""
}

// view is everything the template shows, already formatted. All quantity
// arithmetic happened in package quantity before it was built.
type view struct {
	ID          int64
	Name        string
	Description string
	// Meta is the line under the title; history shows the same for a revision.
	Meta layout.RecipeMeta
	// VideoID is the recipe's YouTube video, empty for none. HasThumbnail says
	// whether a preview image is stored for it; without one the page shows a
	// plain placeholder (ADR-00021).
	VideoID      string
	HasThumbnail bool
	Tags         []string
	// Ribbon is the time ribbon under "Ready in about..."; Empty when no step
	// has a duration.
	Ribbon ribbon.Ribbon
	// Tips are the pinned notes, shown above the steps.
	Tips     []string
	Servings string
	Yield    string

	// Times lists prep, cook and the special times that are set, in that
	// order; Total is the derived total.
	Times []timeRow
	Total string
	// TotalNote says where a derived total came from, so the page shows which
	// rule produced it (ADR-00004).
	TotalNote string

	// Notice is an input problem shown in place of a bare error page, for
	// requests made by htmx.
	Notice string

	Scaled     bool
	Multiplier string
	Opts       display.Options

	Components []componentView
	Equipment  []equipmentView

	// Origin is the recipe and version this one was cloned from; nil when it
	// is not a clone or the original is gone.
	Origin *originView
}

type originView struct {
	RecipeID int64
	Version  int64
}

type timeRow struct {
	Label string
	Text  string
	Hint  string
}

type componentView struct {
	Name  string
	Lines []display.Line
	Steps []layout.RecipeStepView
	// FirstStep is the number of the component's first step, counted across
	// components in reading order.
	FirstStep int
}

type equipmentView struct {
	// Text is the line as written ("2 baking sheet"); Name is the catalog name
	// alone, which picks the chip's icon (ADR-00024).
	Text string
	Name string
	Hint string
}

// buildView resolves catalog references and applies opts to every line.
func buildView(ctx context.Context, cat Catalog, id int64, r recipe.Recipe, o display.Options) (view, error) {
	v := view{
		ID: id, Name: r.Name, Description: r.Description, Meta: layout.NewRecipeMeta(r),
		VideoID: r.VideoID, Tags: r.Tags,
		Scaled: o.Scaled(), Multiplier: o.Mult.String(), Opts: o,
	}
	var steps []recipe.Step
	for _, ref := range r.LinearOrder() {
		steps = append(steps, r.Components[ref.ComponentIndex].Steps[ref.Position-1])
	}
	v.Ribbon = ribbon.Build(steps)
	var err error
	if v.Servings, err = display.ScaleQty(r.Servings, o); err != nil {
		return view{}, fmt.Errorf("servings: %w", err)
	}
	yq, err := display.ScaleQty(r.Yield.Qty, o)
	if err != nil {
		return view{}, fmt.Errorf("yield: %w", err)
	}
	v.Yield = strings.TrimSpace(yq + " " + r.Yield.Unit)
	times, err := display.BuildTimes(r)
	if err != nil {
		return view{}, err
	}
	for _, t := range times.Rows {
		row := timeRow{Label: t.Label, Text: t.Text}
		if t.Scales && v.Scaled {
			row.Hint = hintTime
		}
		v.Times = append(v.Times, row)
	}
	v.Total, v.TotalNote = times.Total, times.TotalNote

	number := display.StepNumbers(r)
	guidance, err := display.BuildMeanwhile(r, number)
	if err != nil {
		return view{}, err
	}

	lines := display.NewLineBuilder(cat, o)
	for _, c := range r.Components {
		cv := componentView{Name: c.Name}
		if len(c.Steps) > 0 {
			cv.FirstStep = number[c.Steps[0].ID]
		}
		for _, l := range c.Lines {
			lv, err := lines.Build(ctx, l)
			if err != nil {
				return view{}, err
			}
			cv.Lines = append(cv.Lines, lv)
		}
		for _, s := range c.Steps {
			cv.Steps = append(cv.Steps, layout.NewRecipeStepView(s, number, guidance[s.ID]))
		}
		v.Components = append(v.Components, cv)
	}

	for _, link := range r.Equipment {
		text, err := display.EquipmentText(ctx, cat, link)
		if err != nil {
			return view{}, err
		}
		// The text helper does not return the row, and the hint needs its sized
		// flag; a second primary-key read keeps one formatter for the line.
		eq, err := cat.GetEquipment(ctx, int64(link.Equipment))
		if err != nil {
			return view{}, display.Opaque(err, fmt.Sprintf("equipment %d", link.Equipment))
		}
		v.Equipment = append(v.Equipment, equipmentView{Text: text, Name: eq.Name, Hint: equipmentHint(o.Mult.Cmp(quantity.Int(1)) > 0, eq.Sized)})
	}
	return v, nil
}

// withJournal adds what the journal contributes to the page: the pinned notes
// as tips, and how many times the recipe was made.
func (v *view) withJournal(notes []store.Note, made []store.MadeIt) {
	for _, n := range notes {
		if n.Pinned {
			v.Tips = append(v.Tips, n.Body)
		}
	}
	v.Meta.Made = len(made)
}

// readyIn is the time the eyebrow above the ribbon states: the derived total
// and nothing else. The ribbon's own sum of timed steps is not a total: it
// skips untimed steps and counts parallel ones twice, and ADR-00004 allows only
// the three total rules, of which TotalNote says which applied.
func (v view) readyIn() string { return v.Total }

// hasLines reports whether any component has an ingredient line, so a recipe
// of steps alone shows no empty Ingredients heading.
func (v view) hasLines() bool {
	for _, c := range v.Components {
		if len(c.Lines) > 0 {
			return true
		}
	}
	return false
}
