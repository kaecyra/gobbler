package recipeview

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
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
	Opts       display.Options

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
	Lines []display.Line
	Steps []stepView
	// FirstStep is the number of the component's first step, counted across
	// components in reading order.
	FirstStep int
}

type stepView struct {
	Text string
	// Note is the duration and the passive marker, "20 min, passive".
	Note string
	// Meanwhile is work offered while this passive step runs (ADR-00004);
	// empty for every other step and for recipes without a step graph.
	Meanwhile []display.MeanwhileItem
}

type equipmentView struct {
	Text string
}

// buildView resolves catalog references and applies opts to every line.
func buildView(ctx context.Context, cat Catalog, id int64, r recipe.Recipe, o display.Options) (view, error) {
	v := view{
		ID: id, Name: r.Name, Description: r.Description, Author: r.Author,
		SourceURL: r.SourceURL, Tags: r.Tags, Rating: r.Rating, Calories: r.CaloriesPerServing,
		Scaled: o.Scaled(), Multiplier: o.Mult.String(), Opts: o,
	}
	var err error
	if v.Servings, err = display.ScaleQty(r.Servings, o); err != nil {
		return view{}, fmt.Errorf("servings: %w", err)
	}
	yq, err := display.ScaleQty(r.Yield.Qty, o)
	if err != nil {
		return view{}, fmt.Errorf("yield: %w", err)
	}
	v.Yield = strings.TrimSpace(yq + " " + r.Yield.Unit)
	if err := fillTimes(&v, r); err != nil {
		return view{}, err
	}

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
			cv.Steps = append(cv.Steps, stepView{Text: s.Text, Note: display.StepNote(s), Meanwhile: guidance[s.ID]})
		}
		v.Components = append(v.Components, cv)
	}

	// No equipment hint ("may need a larger pan", ADR-00005): it applies only to
	// equipment with a size and the schema has none yet (issue #85).
	for _, link := range r.Equipment {
		text, err := display.EquipmentText(ctx, cat, link)
		if err != nil {
			return view{}, err
		}
		v.Equipment = append(v.Equipment, equipmentView{Text: text})
	}
	return v, nil
}

func fillTimes(v *view, r recipe.Recipe) error {
	t := r.Times
	add := func(label string, d time.Duration, hinted bool) {
		if d <= 0 {
			return
		}
		row := timeRow{Label: label, Text: display.FormatDuration(d)}
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
	v.Total = display.FormatDuration(total.Duration)
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
