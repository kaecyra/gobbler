package recipeview

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/a-h/templ"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/recipe/display"
	"github.com/kaecyra/gobbler/internal/recipepath"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// A pending proposal to change the recipe shows inline on the household's
// recipe page: a banner with the reason and who made it, and each changed
// element marked in place with its old and new values (ADR-00035). Only this
// page shows it. Cook mode and the copy a share sends read the live recipe.

//go:embed assets/proposal.css
var proposalStylesheet string

// proposalStyles is the stylesheet of the banner and the marks, written into the
// page only when there is a proposal to show.
func proposalStyles() templ.Component {
	return templ.Raw("<style>" + proposalStylesheet + "</style>")
}

// rejectRoute is the path below Pattern that rejects the pending proposal.
const rejectRoute = "/proposal/reject"

// fieldProposal is the form field naming the proposal being rejected, so the one
// the person saw is the one turned down even if a newer one has replaced it.
const fieldProposal = "proposal"

// staleBanner is shown in place of the marks when the recipe was changed after
// the proposal was made.
const staleBanner = "This proposal is out of date: the recipe was changed after it was made, so it cannot be reviewed."

// proposalView is what the page shows of a pending proposal.
type proposalView struct {
	Token, By, Reason string
	// Stale says the recipe moved on after the proposal was made: the banner
	// offers Reject only and nothing is marked.
	Stale bool
	// Changes is how many differences the marks show.
	Changes int
	// Fields are the differences in the recipe's own fields, its parts and its
	// equipment, shown under the title.
	Fields []fieldMark
	// Comps holds the marks of each part, in the order of the recipe's parts.
	Comps []compMarks
	// NewParts are parts the proposal adds.
	NewParts []newPart
}

// fieldMark is one field, part or piece of equipment that differs.
type fieldMark struct{ Label, Old, New string }

type newPart struct {
	Name         string
	Lines, Steps []string
}

// lineMark marks a line of the live recipe: the proposal changes, moves or
// removes it. Kind is empty for a line it leaves alone.
type lineMark struct {
	Kind          recipe.ChangeKind
	New           display.Line
	Edited, Moved bool
}

// stepMark marks a step of the live recipe. Edited says its content differs.
type stepMark struct {
	Kind          recipe.ChangeKind
	New           string
	Edited, Moved bool
	NewNumber     int
}

// compMarks is everything marked in one part of the live recipe. After lists
// what the proposal adds, by the index of the live line or step it follows;
// First is what it adds ahead of the first.
type compMarks struct {
	Lines      []lineMark
	LinesFirst []display.Line
	LinesAfter map[int][]display.Line
	Steps      []stepMark
	StepsFirst []string
	StepsAfter map[int][]string
}

func (p *proposalView) comp(i int) compMarks {
	if p == nil || i >= len(p.Comps) {
		return compMarks{}
	}
	return p.Comps[i]
}

func (c compMarks) line(i int) lineMark {
	if i < len(c.Lines) {
		return c.Lines[i]
	}
	return lineMark{}
}

func (c compMarks) step(i int) stepMark {
	if i < len(c.Steps) {
		return c.Steps[i]
	}
	return stepMark{}
}

func (p *proposalView) marked() bool { return p != nil && !p.Stale }

func reviewHref(id int64, token string) string {
	return editHref(id) + "?" + url.Values{fieldProposal: {token}}.Encode()
}

func rejectHref(id int64) string { return recipepath.Base(id) + rejectRoute }

func changeLabel(n int) string {
	if n == 1 {
		return "1 change"
	}
	return strconv.Itoa(n) + " changes"
}

// buildProposal marks the live recipe with the pending proposal p. A stale
// proposal gets its banner and no marks.
func buildProposal(ctx context.Context, cat Catalog, live recipe.Recipe, p apirecipes.Proposal, version int64) (*proposalView, error) {
	pv := &proposalView{Token: p.Token, By: p.Describe(), Reason: p.Reason, Stale: p.StaleAt(version)}
	if pv.Stale {
		return pv, nil
	}
	d := recipe.Compare(live, *p.Edit)
	pv.Changes = d.Count()
	lines := display.NewLineBuilder(cat, display.Default())
	built := func(l recipe.IngredientLine) (display.Line, error) { return lines.Build(ctx, l) }
	text := func(l recipe.IngredientLine) (string, error) {
		b, err := built(l)
		return b.Text, err
	}

	for _, f := range d.Fields {
		pv.Fields = append(pv.Fields, fieldMark{Label: f.Label, Old: f.Old, New: f.New})
	}
	for _, c := range d.Components {
		switch c.Kind {
		case recipe.Removed:
			pv.Fields = append(pv.Fields, fieldMark{Label: "Part removed", Old: partLabel(c.OldName)})
		case recipe.Changed:
			pv.Fields = append(pv.Fields, fieldMark{Label: "Part renamed", Old: partLabel(c.OldName), New: partLabel(c.NewName)})
		}
	}
	for _, e := range d.Equipment {
		fm := fieldMark{Label: "Equipment"}
		var err error
		if e.Old != nil {
			if fm.Old, err = equipmentText(ctx, cat, *e.Old); err != nil {
				return nil, err
			}
		}
		if e.New != nil {
			if fm.New, err = equipmentText(ctx, cat, *e.New); err != nil {
				return nil, err
			}
		}
		pv.Fields = append(pv.Fields, fm)
	}

	// Where each live line and step sits, by key.
	type spot struct{ comp, at int }
	lineAt, stepAt := map[string]spot{}, map[recipe.StepID]spot{}
	pv.Comps = make([]compMarks, len(live.Components))
	for ci, c := range live.Components {
		pv.Comps[ci] = compMarks{
			Lines: make([]lineMark, len(c.Lines)), LinesAfter: map[int][]display.Line{},
			Steps: make([]stepMark, len(c.Steps)), StepsAfter: map[int][]string{},
		}
		for li, l := range c.Lines {
			lineAt[l.Key] = spot{ci, li}
		}
		for si, s := range c.Steps {
			stepAt[s.ID] = spot{ci, si}
		}
	}
	for _, c := range d.Lines {
		if c.Old == nil {
			continue
		}
		at := lineAt[c.Old.Key]
		m := lineMark{Kind: c.Kind, Edited: c.Edited, Moved: c.Moved}
		if c.New != nil {
			b, err := built(*c.New)
			if err != nil {
				return nil, err
			}
			m.New = b
		}
		pv.Comps[at.comp].Lines[at.at] = m
	}
	for _, c := range d.Steps {
		if c.Old == nil {
			continue
		}
		at := stepAt[c.Old.ID]
		m := stepMark{Kind: c.Kind, Edited: c.Edited, Moved: c.Moved, NewNumber: c.NewNumber}
		if c.New != nil {
			m.New = c.New.Text
		}
		pv.Comps[at.comp].Steps[at.at] = m
	}

	// What the proposal adds goes after the live element that precedes it in the
	// proposal, so an added step reads where it would sit.
	for nj, nc := range p.Edit.Components {
		oi := d.NewToOld[nj]
		if oi < 0 {
			part := newPart{Name: nc.Name}
			for _, l := range nc.Lines {
				t, err := text(l)
				if err != nil {
					return nil, err
				}
				part.Lines = append(part.Lines, t)
			}
			for _, s := range nc.Steps {
				part.Steps = append(part.Steps, s.Text)
			}
			pv.NewParts = append(pv.NewParts, part)
			continue
		}
		marks := &pv.Comps[oi]
		after := -1
		for _, l := range nc.Lines {
			if at, ok := lineAt[l.Key]; ok && l.Key != "" && at.comp == oi {
				after = at.at
				continue
			}
			t, err := built(l)
			if err != nil {
				return nil, err
			}
			if after < 0 {
				marks.LinesFirst = append(marks.LinesFirst, t)
			} else {
				marks.LinesAfter[after] = append(marks.LinesAfter[after], t)
			}
		}
		after = -1
		for _, s := range nc.Steps {
			if at, ok := stepAt[s.ID]; ok && at.comp == oi {
				after = at.at
				continue
			}
			if after < 0 {
				marks.StepsFirst = append(marks.StepsFirst, s.Text)
			} else {
				marks.StepsAfter[after] = append(marks.StepsAfter[after], s.Text)
			}
		}
	}
	return pv, nil
}

func partLabel(name string) string {
	if name == "" {
		return "(unnamed)"
	}
	return name
}

func equipmentText(ctx context.Context, cat Catalog, link recipe.EquipmentLink) (string, error) {
	e, err := display.ResolveEquipment(ctx, cat, link)
	if err != nil {
		return "", fmt.Errorf("equipment %d: %w", link.Equipment, err)
	}
	return e.Text, nil
}

// proposalFor returns what the page shows of the recipe's pending proposal, nil
// when it has none.
func proposalFor(ctx context.Context, d Deps, id int64, live recipe.Recipe, version int64) (*proposalView, error) {
	p, found, err := d.Recipes.PendingEdit(ctx, caller.FromContext(ctx), id)
	if err != nil {
		return nil, fmt.Errorf("load pending proposal of recipe %d: %w", id, err)
	}
	if !found {
		return nil, nil
	}
	pv, err := buildProposal(ctx, d.Catalog, live, p, version)
	if err != nil {
		return nil, fmt.Errorf("build proposal %s of recipe %d: %w", p.Token, id, err)
	}
	return pv, nil
}

// reject turns the proposal the form names down. The recipe is not touched.
func reject(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := recipeID(r)
		if err != nil {
			layout.WriteError(w, r, err)
			return
		}
		token := r.PostFormValue(fieldProposal)
		if token == "" {
			layout.WriteError(w, r, fmt.Errorf("%w: no proposal named", layout.ErrInvalid))
			return
		}
		p, err := d.Recipes.GetProposal(r.Context(), caller.FromContext(r.Context()), token)
		if err != nil {
			layout.WriteError(w, r, fmt.Errorf("load proposal: %w", err))
			return
		}
		if p.Kind != apirecipes.ProposalEdit || p.RecipeID != id {
			layout.WriteError(w, r, fmt.Errorf("proposal is not one for recipe %d: %w", id, layout.ErrNotFound))
			return
		}
		if err := d.Recipes.RejectProposal(r.Context(), caller.FromContext(r.Context()), token); err != nil {
			layout.WriteError(w, r, fmt.Errorf("reject proposal: %w", err))
			return
		}
		// The title is not at hand; the view page completes the slug with a 302.
		http.Redirect(w, r, recipepath.View(id, ""), http.StatusSeeOther)
	}
}

func (c compMarks) hasLineAdds() bool { return len(c.LinesFirst) > 0 || len(c.LinesAfter) > 0 }

func (c compMarks) hasStepAdds() bool { return len(c.StepsFirst) > 0 || len(c.StepsAfter) > 0 }
