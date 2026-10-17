package recipeview

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/ribbon"
)

// mealLabels name the meal types on the page, in the library's order.
var mealLabels = map[recipe.MealType]string{
	recipe.MealBreakfast: "Breakfast", recipe.MealMain: "Main", recipe.MealSide: "Side",
	recipe.MealDessert: "Dessert", recipe.MealSnack: "Snack", recipe.MealDrink: "Drink",
}

// heatLabels are indexed by spice level; 0 is nothing to say.
var heatLabels = [...]string{"", "Mild heat", "Medium heat", "Hot"}

// verdicts are the words of the spoon scale, indexed by rating; they are the
// accessible name of the spoons.
var verdicts = [...]string{"Not rated", "Not again", "It was fine", "Good", "Make it again", "House favourite"}

// maxSpoons is the top of the rating scale.
const maxSpoons = 5

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
	Meta Meta
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
	Steps []StepView
	// FirstStep is the number of the component's first step, counted across
	// components in reading order.
	FirstStep int
}

// StepView is one step as the recipe page and a past version show it.
type StepView struct {
	// Title is the step's short heading, empty when it has none.
	Title string
	Text  string
	// Passive marks a step that needs no attention while it runs.
	Passive bool
	// After lists the numbers of the steps this one waits for.
	After []int
	// Note is the duration and the passive marker, "20 min, passive".
	Note string
	// Meanwhile is work offered while this passive step runs (ADR-00004);
	// empty for every other step and for recipes without a step graph.
	Meanwhile []display.MeanwhileItem
}

type equipmentView struct {
	Text string
	Hint string
}

// buildView resolves catalog references and applies opts to every line.
func buildView(ctx context.Context, cat Catalog, id int64, r recipe.Recipe, o display.Options) (view, error) {
	v := view{
		ID: id, Name: r.Name, Description: r.Description, Meta: NewMeta(r),
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
			cv.Steps = append(cv.Steps, NewStepView(s, number, guidance[s.ID]))
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
		v.Equipment = append(v.Equipment, equipmentView{Text: text, Hint: equipmentHint(o.Mult.Cmp(quantity.Int(1)) > 0, eq.Sized)})
	}
	return v, nil
}

// NewStepView formats one step. number is display.StepNumbers of the recipe,
// which turns each dependency into the number printed on its step; meanwhile
// is the guidance offered while it runs, nil for none.
func NewStepView(s recipe.Step, number map[recipe.StepID]int, meanwhile []display.MeanwhileItem) StepView {
	sv := StepView{Title: s.Title, Text: s.Text, Passive: s.Passive, Note: display.StepNote(s), Meanwhile: meanwhile}
	for _, dep := range s.DependsOn {
		sv.After = append(sv.After, number[dep])
	}
	return sv
}

// Meta is what sits in the line under a recipe's title: meal type, spice,
// rating, made count, author, calories and source. The recipe page and the
// page of a past version build it the same way, so the two cannot drift.
type Meta struct {
	// MealType is the meal type's key and Meal its label, both empty when
	// unset. Spice is 0 to 3 and 0 shows nothing. Spoons is the rating, 0 for
	// unrated. Made is the length of the made-it log; a past version has none.
	MealType   recipe.MealType
	Meal       string
	Spice      int
	Spoons     int
	Made       int
	Author     string
	Calories   *int
	SourceURL  string
	SourceHost string
}

// NewMeta reads the fields of r that the meta row shows.
func NewMeta(r recipe.Recipe) Meta {
	m := Meta{
		MealType: r.MealType, Meal: mealLabels[r.MealType], Author: r.Author,
		Calories: r.CaloriesPerServing, SourceURL: r.SourceURL, SourceHost: sourceHost(r.SourceURL),
	}
	if r.SpiceLevel != nil && *r.SpiceLevel > 0 {
		m.Spice = min(*r.SpiceLevel, recipe.MaxSpiceLevel)
	}
	if r.Rating != nil && *r.Rating > 0 {
		m.Spoons = min(*r.Rating, maxSpoons)
	}
	return m
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

// sourceHost is the host of a source address for the link text, empty when the
// address does not parse or has none.
func sourceHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// heatLabel and verdict name the chilis and the spoons for screen readers.
func heatLabel(level int) string { return heatLabels[level] }

func verdict(spoons int) string { return verdicts[spoons] }

// afterText renders "step 3" or "steps 3, 4, 5" for the steps a step waits
// for.
func afterText(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	if len(parts) == 1 {
		return "after step " + parts[0]
	}
	return "after steps " + strings.Join(parts, ", ")
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
