package recipeview

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// hintTime is shown on cook and special times of a scaled view (ADR-00005):
// times are not scaled, so the page says they may need attention.
const hintTime = "time may change"

// view is everything the template shows, already formatted. All quantity
// arithmetic happened in package quantity before it was built.
type view struct {
	ID          int64
	Name        string
	Description string
	Author      string
	SourceURL   string
	Tags        []string
	Rating      *int
	Calories    *int
	Servings    string
	Yield       string

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
	Opts       options

	Components []componentView
	Equipment  []equipmentView
}

type timeRow struct {
	Label string
	Text  string
	Hint  string
}

type componentView struct {
	Name  string
	Lines []lineView
	Steps []stepView
	// FirstStep is the number of the component's first step, counted across
	// components in reading order.
	FirstStep int
}

type lineView struct {
	Text string
	// Exact is the unrounded amount, shown on hover, empty when nothing was
	// rounded.
	Exact string
	// Unscaled marks a line whose ingredient is unresolved: its original text
	// is shown and no scaling or conversion applies to it.
	Unscaled bool
}

type stepView struct {
	Text string
	// Note is the duration and the passive marker, "20 min, passive".
	Note string
	// Meanwhile is work offered while this passive step runs (ADR-00004);
	// empty for every other step and for recipes without a step graph.
	Meanwhile []meanwhileItem
}

type equipmentView struct {
	Text string
}

// buildView resolves catalog references and applies opts to every line.
func buildView(ctx context.Context, cat Catalog, id int64, r recipe.Recipe, o options) (view, error) {
	v := view{
		ID: id, Name: r.Name, Description: r.Description, Author: r.Author,
		SourceURL: r.SourceURL, Tags: r.Tags, Rating: r.Rating, Calories: r.CaloriesPerServing,
		Scaled: o.scaled(), Multiplier: o.mult.String(), Opts: o,
	}
	var err error
	if v.Servings, err = scaleQty(r.Servings, o); err != nil {
		return view{}, fmt.Errorf("servings: %w", err)
	}
	yq, err := scaleQty(r.Yield.Qty, o)
	if err != nil {
		return view{}, fmt.Errorf("yield: %w", err)
	}
	v.Yield = strings.TrimSpace(yq + " " + r.Yield.Unit)
	if err := fillTimes(&v, r); err != nil {
		return view{}, err
	}

	number := stepNumbers(r)
	guidance, err := buildMeanwhile(r, number)
	if err != nil {
		return view{}, err
	}

	ingredients := map[recipe.IngredientID]catalog.Ingredient{}
	for _, c := range r.Components {
		cv := componentView{Name: c.Name}
		if len(c.Steps) > 0 {
			cv.FirstStep = number[c.Steps[0].ID]
		}
		for _, l := range c.Lines {
			lv, err := buildLine(ctx, cat, ingredients, l, o)
			if err != nil {
				return view{}, err
			}
			cv.Lines = append(cv.Lines, lv)
		}
		for _, s := range c.Steps {
			cv.Steps = append(cv.Steps, stepView{Text: s.Text, Note: stepNote(s), Meanwhile: guidance[s.ID]})
		}
		v.Components = append(v.Components, cv)
	}

	// No equipment hint ("may need a larger pan", ADR-00005): it applies only to
	// equipment with a size and the schema has none yet (issue #85).
	for _, link := range r.Equipment {
		eq, err := cat.GetEquipment(ctx, int64(link.Equipment))
		if err != nil {
			return view{}, opaque(err, fmt.Sprintf("equipment %d", link.Equipment))
		}
		v.Equipment = append(v.Equipment, equipmentView{Text: strings.TrimSpace(link.Qty.String() + " " + eq.Name)})
	}
	return v, nil
}

// scaleQty scales a quantity linearly and renders it. Servings and yield
// have no unit, so none of the unit modes apply.
func scaleQty(q quantity.Quantity, o options) (string, error) {
	a, err := quantity.Scale(quantity.Amount{Qty: q}, quantity.RuleLinear, o.mult)
	if err != nil {
		return "", err
	}
	if !o.scaled() {
		return a.Qty.String(), nil
	}
	r, err := quantity.Round(a)
	if err != nil {
		return "", err
	}
	return r.Rounded.Qty.String(), nil
}

func fillTimes(v *view, r recipe.Recipe) error {
	t := r.Times
	add := func(label string, d time.Duration, hinted bool) {
		if d <= 0 {
			return
		}
		row := timeRow{Label: label, Text: formatDuration(d)}
		if hinted && v.Scaled {
			row.Hint = hintTime
		}
		v.Times = append(v.Times, row)
	}
	add("Prep", t.Prep, false)
	add("Cook", t.Cook, true)
	for _, s := range t.Special {
		add(s.Label, s.Duration, true)
	}
	total, err := r.TotalTime()
	if err != nil {
		return fmt.Errorf("total time: %w", err)
	}
	v.Total = formatDuration(total.Duration)
	switch total.Source {
	case recipe.TotalStored:
		v.TotalNote = "as stated by the source"
	case recipe.TotalCriticalPath:
		v.TotalNote = "from the longest chain of steps"
	case recipe.TotalSum:
		v.TotalNote = "prep, cook and waiting times added"
	}
	return nil
}

func buildLine(ctx context.Context, cat Catalog, cache map[recipe.IngredientID]catalog.Ingredient, l recipe.IngredientLine, o options) (lineView, error) {
	if l.Ingredient == 0 {
		return lineView{Text: l.Raw, Unscaled: true}, nil
	}
	ing, ok := cache[l.Ingredient]
	if !ok {
		var err error
		if ing, err = cat.GetIngredient(ctx, int64(l.Ingredient)); err != nil {
			return lineView{}, opaque(err, fmt.Sprintf("ingredient %d", l.Ingredient))
		}
		cache[l.Ingredient] = ing
	}

	amount, err := quantity.Scale(l.Amount, l.Rule, o.mult)
	if err != nil {
		return lineView{}, fmt.Errorf("scale %q: %w", l.Raw, err)
	}
	density, err := density(ing)
	if err != nil {
		return lineView{}, err
	}
	amount, err = quantity.Display(amount, o.mode, density)
	if err != nil {
		return lineView{}, fmt.Errorf("convert %q: %w", l.Raw, err)
	}
	var lv lineView
	shown := amount
	if o.scaled() || o.mode != quantity.ModeAsWritten {
		rounded, err := quantity.Round(amount)
		if err != nil {
			return lineView{}, fmt.Errorf("round %q: %w", l.Raw, err)
		}
		shown = rounded.Rounded
		if exact := rounded.Exact.String(); exact != shown.String() {
			lv.Exact = exact
		}
	}

	parts := []string{shown.String(), ing.Name}
	text := strings.Join(nonEmpty(parts), " ")
	var quals []string
	for _, q := range l.Qualifiers {
		quals = append(quals, q.Text)
	}
	if len(quals) > 0 {
		text += ", " + strings.Join(quals, ", ")
	}
	if l.Rule == quantity.RuleToTaste {
		text += " to taste"
	}
	if l.Optional {
		text += " (optional)"
	}
	lv.Text = text
	return lv, nil
}

// density returns the ingredient's grams per millilitre, or the zero Rat when
// the catalog has none (quantity then leaves volume as volume).
func density(ing catalog.Ingredient) (quantity.Rat, error) {
	if ing.DensityGPerML == nil {
		return quantity.Rat{}, nil
	}
	d, err := quantity.ParseRat(strconv.FormatFloat(*ing.DensityGPerML, 'f', -1, 64))
	if err != nil {
		return quantity.Rat{}, fmt.Errorf("density of %q: %w", ing.Name, err)
	}
	return d, nil
}

func nonEmpty(in []string) []string {
	out := in[:0:0]
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// formatDuration renders "1 h 30 min"; a zero duration is "" (unknown).
func formatDuration(d time.Duration) string {
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

func stepNote(s recipe.Step) string {
	var parts []string
	if d := formatDuration(s.Duration); d != "" {
		parts = append(parts, d)
	}
	if s.Passive {
		parts = append(parts, "passive")
	}
	return strings.Join(parts, ", ")
}
