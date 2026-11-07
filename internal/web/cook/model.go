package cook

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/recipepath"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// page is everything the template shows, already formatted.
type page struct {
	ID   int64
	Name string
	// ExitHref returns to the recipe view with the same scale and units.
	ExitHref string
	// Meta is the scaled servings and the unit mode, such as "Serves 4 ·
	// Metric"; the servings part is left out when the recipe has none.
	Meta string

	// Empty is true when the recipe has no steps; the other fields are then
	// unset.
	Empty bool

	Current   int
	Total     int
	Step      stepView
	PrevHref  string
	NextHref  string
	StepList  []listItem
	Component string
	// IsMeanwhile is true when this step is offered as work to do while an
	// earlier passive step runs.
	IsMeanwhile bool
	Lines       []display.Line
	Equipment   []equipmentChip
	Meanwhile   []meanwhileLink
	// UpNext is the step after this one, named by its title when it has one,
	// else its text; nil on the last step.
	UpNext *listItem
	// Notes are the recipe author's notes, the same on every step: cook mode
	// shows one step at a time, so it reaches them through a button
	// (ADR-00041).
	Notes []noteView
}

// noteView is one author note: Lead is the bold title with its colon, empty
// when the note has none.
type noteView struct {
	Lead string
	Text string
}

// equipmentChip is one piece of equipment on a step: the line as written
// ("2 baking sheet") and the catalog name alone, which picks its icon
// (ADR-00024).
type equipmentChip struct {
	Text string
	Name string
}

type stepView struct {
	Number int
	Text   string
	// Title is the step's short heading, empty when it has none.
	Title string
	// Label names the step's timer: its title, else its text as plain text
	// (layout.MarkdownText).
	Label string
	Note  string
	// Seconds is the step's duration, 0 when unknown: no timer is offered.
	Seconds int
}

// meanwhileLink is a piece of work offered during a passive step, linked to
// its own step page.
type meanwhileLink struct {
	Number int
	// Title is the step's title, empty when it has none.
	Title string
	Text  string
	Href  string
}

type listItem struct {
	Number  int
	Title   string
	Text    string
	Href    string
	Current bool
	// Done is true for the steps before the current one.
	Done bool
}

// buildPage resolves the step to show and everything relevant to it. A step
// number past the end is an invalid request, not a missing page.
func buildPage(ctx context.Context, cat Catalog, id int64, r recipe.Recipe, o display.Options, n int) (page, error) {
	p := page{ID: id, Name: r.Name, ExitHref: exitHref(id, r.Name, o)}
	servings, err := display.ScaleQty(r.Servings, o)
	if err != nil {
		return page{}, fmt.Errorf("servings: %w", err)
	}
	p.Meta = unitsLabel(o.Units)
	if servings != "" {
		p.Meta = "Serves " + servings + " · " + p.Meta
	}
	refs := r.LinearOrder()
	p.Total = len(refs)
	if p.Total == 0 {
		p.Empty = true
		return p, nil
	}
	if n > p.Total {
		return page{}, fmt.Errorf("%w: step %d of %d: %w", layout.ErrInvalid, n, p.Total, errStepOutOfRange)
	}
	for _, an := range r.AuthorNotes {
		p.Notes = append(p.Notes, noteView{Lead: an.Lead(), Text: an.Text})
	}
	p.Current = n
	ref := refs[n-1]
	comp := r.Components[ref.ComponentIndex]
	step := comp.Steps[ref.Position-1]

	p.Step = stepView{Number: n, Text: step.Text, Title: step.Title, Label: step.Title, Note: display.StepNote(step), Seconds: int(step.Duration / time.Second)}
	if p.Step.Label == "" {
		// The label is plain text (a button name, the alert, the page
		// title), so it is the rendered step's text, not its markdown.
		if p.Step.Label, err = layout.MarkdownText(step.Text); err != nil {
			return page{}, fmt.Errorf("step %d timer label: %w", n, err)
		}
	}
	p.Component = comp.Name
	if n > 1 {
		p.PrevHref = stepHref(id, n-1, o)
	}
	if n < p.Total {
		p.NextHref = stepHref(id, n+1, o)
	}
	for i, rf := range refs {
		s := r.Components[rf.ComponentIndex].Steps[rf.Position-1]
		p.StepList = append(p.StepList, listItem{Number: i + 1, Title: s.Title, Text: s.Text, Href: stepHref(id, i+1, o), Current: i+1 == n, Done: i+1 < n})
	}

	if n < p.Total {
		next := p.StepList[n]
		p.UpNext = &next
	}

	// Ingredients belong to components, not to steps, so the step shows its
	// own component's lines: the closest relevant set the data model has.
	lines := display.NewLineBuilder(cat, o)
	for _, l := range comp.Lines {
		lv, err := lines.Build(ctx, l)
		if err != nil {
			return page{}, err
		}
		p.Lines = append(p.Lines, lv)
	}

	// Equipment tied to this step, and equipment tied to no step at all (used
	// throughout).
	for _, link := range r.Equipment {
		if !usesStep(link, step.ID) {
			continue
		}
		eq, err := display.ResolveEquipment(ctx, cat, link)
		if err != nil {
			return page{}, err
		}
		p.Equipment = append(p.Equipment, equipmentChip{Text: eq.Text, Name: eq.Name})
	}

	guidance, err := display.BuildMeanwhile(r, display.StepNumbers(r))
	if err != nil {
		return page{}, err
	}
	for _, it := range guidance[step.ID] {
		title := p.StepList[it.Number-1].Title
		p.Meanwhile = append(p.Meanwhile, meanwhileLink{Number: it.Number, Title: title, Text: it.Text, Href: stepHref(id, it.Number, o)})
	}
	// The next step is often the first piece of meanwhile work; it is named
	// once, under Meanwhile.
	for _, m := range p.Meanwhile {
		if p.UpNext != nil && m.Number == p.UpNext.Number {
			p.UpNext = nil
		}
	}
	for _, items := range guidance {
		for _, it := range items {
			if it.Number == n {
				p.IsMeanwhile = true
			}
		}
	}
	return p, nil
}

// unitsLabel names a unit mode with the words of the recipe view's unit
// buttons.
func unitsLabel(units string) string {
	switch units {
	case display.UnitsMetric:
		return "Metric"
	case display.UnitsWeight:
		return "Weight"
	}
	return "As written"
}

func usesStep(link recipe.EquipmentLink, id recipe.StepID) bool {
	if len(link.Steps) == 0 {
		return true
	}
	for _, s := range link.Steps {
		if s == id {
			return true
		}
	}
	return false
}

func exitHref(id int64, name string, o display.Options) string {
	return o.URL(recipepath.View(id, name), nil)
}

func stepHref(id int64, n int, o display.Options) string {
	return o.URL(recipepath.Base(id)+"/cook", url.Values{paramStep: {strconv.Itoa(n)}})
}

// timerLabel renders a duration given in seconds, such as "45 min".
func timerLabel(seconds int) string {
	return display.FormatDuration(time.Duration(seconds) * time.Second)
}
