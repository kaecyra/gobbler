package editor

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	apiingest "github.com/kaecyra/gobbler/internal/api/ingest"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// FormID is the id of the editor form (editor.templ). A control outside the
// form, such as the picture picker's radios, joins it through its form
// attribute. Go and templ use this constant; internal/web/static/editor.js
// (getElementById) and the e2e specs (form#editor) repeat the literal and
// must follow a rename.
const FormID = "editor"

// CancelFormID is the id of the form the page hosting the review must provide
// next to the editor form: the Cancel button belongs to it through its form
// attribute. The editor form is an htmx form that posts to one URL and swaps
// the answer in place, so a button of its own could not leave for another.
const CancelFormID = "review-cancel"

// ReviewOptions says where a review screen's buttons post, and what the
// parts panel can say about the source.
type ReviewOptions struct {
	// Action is where the form and its structural buttons post.
	Action string
	// SourceParts is how many headings the source listed its ingredients
	// under. While the draft has that many parts, the panel says the parts
	// came from them.
	SourceParts int
	// PartsBeside is set when the page shows the parts panel itself, beside
	// the form (Form.Parts), so the form leaves it out.
	PartsBeside bool
}

// Unsaved hosts the editor on an ingest draft that is not stored yet
// (ADR-00020 decision 3): the review screen is this form, with confirm in
// place of save. It renders the editor's own components, reads the posted form
// with the editor's own parser and checks it with the editor's own build, so a
// draft is held to the rules of a stored recipe. It stores nothing; the caller
// confirms and saves the returned draft through apiingest.Confirm.
type Unsaved struct{ h handler }

// NewUnsaved returns an Unsaved that reads names from c. The ingest screen
// passes the catalog service bound to the request's Caller
// (*apicatalog.Bound), so every read checks the household's membership
// (ADR-00043).
func NewUnsaved(c Catalog) Unsaved { return Unsaved{h: handler{Deps: Deps{Catalog: c}}} }

// Form is the editor form for a draft under review.
type Form struct{ v view }

// Component renders the form. It is a fragment of one <form>, the same one the
// recipe editor swaps in place.
func (f Form) Component() templ.Component { return editorForm(f.v) }

// Parts renders the parts panel for a page that shows it beside the form
// (ReviewOptions.PartsBeside). Its buttons belong to the form.
func (f Form) Parts() templ.Component { return partsPanel(f.v, false) }

// Fragment is the form as an htmx answer: the form, and the parts panel beside
// it swapped out of band when the page shows one, so both follow the edit.
func (f Form) Fragment() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := editorForm(f.v).Render(ctx, w); err != nil {
			return err
		}
		if f.v.Review == nil || !f.v.Review.PartsBeside {
			return nil
		}
		return partsPanel(f.v, true).Render(ctx, w)
	})
}

// NeedsLook is how many items the form marks for a look, the count its save
// bar shows.
func (f Form) NeedsLook() int {
	n, _ := f.v.needsLook()
	return n
}

// WithProblems returns f showing msgs as problems with the recipe as a whole.
func (f Form) WithProblems(msgs ...string) Form {
	f.v.D.Problems = append(append([]string(nil), f.v.D.Problems...), msgs...)
	return f
}

// Open returns the form for d as the rules read it, each flagged item marked.
func (u Unsaved) Open(ctx context.Context, d apiingest.Draft, o ReviewOptions) (Form, error) {
	names, err := u.h.names(ctx)
	if err != nil {
		return Form{}, err
	}
	return Form{v: view{D: fromIngest(d, names), Names: names, Review: &o}}, nil
}

// Outcome is what reading a posted review form produced.
type Outcome struct {
	// Form is the form to show again. It carries the messages of a rejected
	// form, and is meaningful when Ready is false.
	Form Form
	// Ready is true when the form passed the editor's validation and Draft is
	// the draft as the user left it, to be confirmed. A structural button (add,
	// remove, move) or a rejected form leaves it false.
	Ready bool
	// Rejected is true when the form was refused, with its messages in Form;
	// false for a structural button, which only changes what is shown.
	Rejected bool
	Draft    apiingest.Draft
}

// Submit reads a posted review form over base, the draft under review. What
// the form shows comes from the form; everything else (the source, parser
// version, method, scoring and LLM state) is kept from base.
func (u Unsaved) Submit(ctx context.Context, form url.Values, base apiingest.Draft, o ReviewOptions) (Outcome, error) {
	names, err := u.h.names(ctx)
	if err != nil {
		return Outcome{}, err
	}
	d := parseForm(form)
	d.arrange()
	show := func() Outcome { return Outcome{Form: Form{v: view{D: d, Names: names, Review: &o}}} }

	if action := form.Get(fieldAction); action != "" && action != actionSave {
		if err := d.apply(action); err != nil {
			return Outcome{}, fmt.Errorf("%w: %w", layout.ErrInvalid, err)
		}
		return show(), nil
	}
	rec, props, rows, ok, err := u.h.build(ctx, &d, recipe.Recipe{})
	if err != nil {
		return Outcome{}, err
	}
	if !ok {
		rejected := show()
		rejected.Rejected = true
		return rejected, nil
	}
	return Outcome{Ready: true, Draft: toIngest(base, rec, props, rows, names, d), Form: Form{v: view{D: d, Names: names, Review: &o}}}, nil
}

// fromIngest turns a draft into the editor's starting form. A line the catalog
// did not match is offered as a new ingredient, ticked, exactly as the draft
// proposed it; the user can instead type an existing ingredient's name.
func fromIngest(d apiingest.Draft, names lookups) draft {
	low := func(conf float64) bool { return conf < d.Threshold }
	ed := draft{
		Video: d.VideoID, MealType: string(d.MealType), Cuisine: string(d.Cuisine),
		Name: d.Name.Value, Author: d.Author.Value, Description: d.Description.Value,
		YieldQty: qtyText(d.Yield.Value.Qty), YieldUnit: d.Yield.Value.Unit, Servings: qtyText(d.Servings.Value),
		Prep: durationText(d.Prep.Value), Cook: durationText(d.Cook.Value), Total: durationText(d.Total.Value),
		Marks: map[string]mark{},
	}
	if d.SpiceLevel != nil {
		ed.Spice = strconv.Itoa(*d.SpiceLevel)
	}
	if d.Calories.Value > 0 {
		ed.Calories = fmt.Sprint(d.Calories.Value)
	}
	// An LLM mark shows on an empty field too: an agent's revision that
	// cleared it (ADR-00048). The fallback never marks an empty field.
	flag := func(field string, present bool, conf float64, fromLLM bool) {
		if m := (mark{Low: present && low(conf), LLM: fromLLM}); !m.zero() {
			ed.Marks[field] = m
		}
	}
	flag("name", true, d.Name.Confidence, d.Name.FromLLM)
	flag("author", d.Author.Value != "", d.Author.Confidence, d.Author.FromLLM)
	flag("description", d.Description.Value != "", d.Description.Confidence, d.Description.FromLLM)
	flag("yield", ed.YieldQty != "" || ed.YieldUnit != "", d.Yield.Confidence, d.Yield.FromLLM)
	flag("prep", d.Prep.Value > 0, d.Prep.Confidence, d.Prep.FromLLM)
	flag("cook", d.Cook.Value > 0, d.Cook.Confidence, d.Cook.FromLLM)
	flag("total", d.Total.Value > 0, d.Total.Confidence, d.Total.FromLLM)
	flag("servings", ed.Servings != "", d.Servings.Confidence, d.Servings.FromLLM)
	flag("calories", d.Calories.Value > 0, d.Calories.Confidence, d.Calories.FromLLM)
	flag("meal_type", false, 0, d.MealTypeFromLLM)
	flag("cuisine", false, 0, d.CuisineFromLLM)

	n := 0 // the draft numbers its steps from 1 across every component
	for _, c := range d.Components {
		cd := compDraft{Name: c.Name}
		for _, l := range c.Lines {
			ld := lineDraft{
				Qty: qtyText(l.Amount.Qty), Unit: l.Amount.Unit.Name,
				Optional: l.Optional, Raw: l.Raw, Rule: string(quantity.RuleLinear),
				Issues: l.Issues, Mark: mark{Low: low(l.Confidence), LLM: l.FromLLM},
			}
			if l.ToTaste {
				ld.Rule = string(quantity.RuleToTaste)
			}
			if l.Amount.HasPackage {
				ld.PkgQty, ld.PkgUnit = qtyText(l.Amount.Package.Qty), l.Amount.Package.Unit.Name
			}
			if l.Ingredient != 0 {
				ld.Ingredient = names.ingredient[int64(l.Ingredient)]
			}
			if ld.Ingredient == "" {
				ld.Ingredient, ld.Propose = l.Name, l.Ingredient == 0
			}
			for _, q := range l.Qualifiers {
				ld.Quals = append(ld.Quals, qualDraft{Kind: string(q.Kind), Text: q.Text})
			}
			cd.Lines = append(cd.Lines, ld)
		}
		for _, s := range c.Steps {
			n++
			sd := stepDraft{
				ID: stepID(n), Text: s.Text, Duration: durationText(s.Duration), Kind: string(s.Kind), Title: s.Title,
				Mark: mark{Low: low(s.Confidence), LLM: s.FromLLM}, Passive: s.Passive,
				Graph: graphMark{Cue: s.GraphCue, LLM: s.GraphFromLLM, User: s.GraphByUser},
			}
			for _, dep := range s.DependsOn {
				sd.Deps = append(sd.Deps, stepID(dep))
			}
			sd.Graph.Read = graphReading(sd.Passive, sd.Deps)
			cd.Steps = append(cd.Steps, sd)
		}
		ed.Components = append(ed.Components, cd)
	}
	for _, n := range d.AuthorNotes {
		ed.Notes = append(ed.Notes, noteDraft{Title: n.Title, Text: n.Text, Issues: n.Issues, Mark: mark{Low: low(n.Confidence), LLM: n.FromLLM}})
	}
	for _, e := range d.Equipment {
		name := e.Name
		if e.Equipment != 0 {
			if n := names.equipment[int64(e.Equipment)]; n != "" {
				name = n
			}
		}
		ed.Equipment = append(ed.Equipment, equipDraft{Name: name, Propose: e.Equipment == 0, Mark: mark{LLM: e.FromLLM}})
	}
	ed.arrange()
	return ed
}

// toIngest puts a recipe the editor built back into a draft, over base, which
// supplies what the form does not edit (source, parser version, method, the
// scoring). props names the lines and equipment the user asked to add as new.
// ed is the form rec was built from: its marks and issues go back into the
// draft, so a draft put back after a failed save reopens with its flags
// (ADR-00006 obligation 4). Only what Draft keeps is copied; the form shows
// only that (see view.carries), so nothing the user edited is lost.
//
// A step's passive flag and dependencies go back as the form has them, the
// dependencies renumbered to the steps' places in the recipe. GraphCue,
// GraphFromLLM and GraphByUser say who read the graph, so they stay only for a
// step whose graph the user left as it was read; a step whose flag or
// dependencies were changed, or that the user added, has the user's reading:
// only GraphByUser is set, so a retry with the LLM leaves it alone.
func toIngest(base apiingest.Draft, rec recipe.Recipe, props []proposal, rows kept, names lookups, ed draft) apiingest.Draft {
	out := base
	out.Name.Value, out.Author.Value, out.Description.Value = rec.Name, rec.Author, rec.Description
	out.Yield.Value, out.Servings.Value = rec.Yield, rec.Servings
	out.VideoID = rec.VideoID
	out.MealType, out.SpiceLevel, out.Cuisine = rec.MealType, rec.SpiceLevel, rec.Cuisine
	out.Calories.Value = 0
	if rec.CaloriesPerServing != nil {
		out.Calories.Value = *rec.CaloriesPerServing
	}
	out.Prep.Value, out.Cook.Value, out.Total.Value = rec.Times.Prep, rec.Times.Cook, rec.Times.Total
	carryMark(&out.Name, ed.mark("name"), base.Threshold)
	carryMark(&out.Author, ed.mark("author"), base.Threshold)
	carryMark(&out.Description, ed.mark("description"), base.Threshold)
	carryMark(&out.Yield, ed.mark("yield"), base.Threshold)
	carryMark(&out.Prep, ed.mark("prep"), base.Threshold)
	carryMark(&out.Cook, ed.mark("cook"), base.Threshold)
	carryMark(&out.Total, ed.mark("total"), base.Threshold)
	carryMark(&out.Servings, ed.mark("servings"), base.Threshold)
	carryMark(&out.Calories, ed.mark("calories"), base.Threshold)
	out.MealTypeFromLLM, out.CuisineFromLLM = ed.mark("meal_type").LLM, ed.mark("cuisine").LLM

	newLine := map[[2]int]string{}
	newEquip := map[int]string{}
	for _, p := range props {
		if p.equipment {
			newEquip[p.index] = p.name
		} else {
			newLine[[2]int{p.component, p.index}] = p.name
		}
	}
	// rows says which form row each recipe row came from, so the form's marks
	// follow their rows past any the build dropped.
	numbers := stepNumbers(rec)
	out.Components = nil
	for ci, c := range rec.Components {
		form := ed.Components[ci]
		dc := apiingest.Component{Name: c.Name}
		for li, l := range c.Lines {
			line := apiingest.Line{
				Raw: l.Raw, Ingredient: l.Ingredient, Amount: l.Amount, Optional: l.Optional,
				ToTaste: l.Rule == quantity.RuleToTaste, Qualifiers: l.Qualifiers,
				Confidence: confidence(form.Lines[rows.lines[ci][li]].Mark, base.Threshold), FromLLM: form.Lines[rows.lines[ci][li]].Mark.LLM, Issues: form.Lines[rows.lines[ci][li]].Issues,
			}
			if name, ok := newLine[[2]int{ci, li}]; ok {
				line.Name = name
			} else {
				line.Name = names.ingredient[int64(l.Ingredient)]
			}
			dc.Lines = append(dc.Lines, line)
		}
		for si, s := range c.Steps {
			fs := form.Steps[rows.steps[ci][si]]
			// The graph provenance stays only while the user left this step's
			// passive flag and dependencies as they were read.
			g := fs.Graph.current(s.Passive, stepIDs(s.DependsOn))
			dc.Steps = append(dc.Steps, apiingest.Step{
				Text: s.Text, Duration: s.Duration,
				Confidence: confidence(fs.Mark, base.Threshold), FromLLM: fs.Mark.LLM,
				Kind: s.Kind, Title: s.Title,
				Passive: s.Passive, DependsOn: dependsOn(s, numbers), GraphCue: g.Cue, GraphFromLLM: g.LLM, GraphByUser: g.User,
			})
		}
		out.Components = append(out.Components, dc)
	}
	out.Equipment = nil
	for ei, e := range rec.Equipment {
		eq := apiingest.Equipment{Equipment: e.Equipment, Name: names.equipment[int64(e.Equipment)], FromLLM: ed.Equipment[rows.equipment[ei]].Mark.LLM}
		if name, ok := newEquip[ei]; ok {
			eq.Name = strings.TrimSpace(name)
		}
		out.Equipment = append(out.Equipment, eq)
	}
	out.AuthorNotes = nil
	for ni, n := range rec.AuthorNotes {
		form := ed.Notes[rows.notes[ni]]
		out.AuthorNotes = append(out.AuthorNotes, apiingest.AuthorNote{
			Title: n.Title, Text: n.Text, Confidence: confidence(form.Mark, base.Threshold), Issues: form.Issues, FromLLM: form.Mark.LLM,
		})
	}
	out.Proposed = apiingest.ProposedFor(out.Components, base.Proposed)
	return out
}

// stepID is the id the editor gives the step a draft numbers n. The ids are
// the draft's numbers while the form is untouched; after that they are only
// names, and stepNumbers maps them back.
func stepID(n int) string { return fmt.Sprintf("s%d", n) }

// stepNumbers numbers the steps of rec from 1 in reading order across every
// component, as ingest.Step.DependsOn does, so a dependency written against a
// step's id follows the step through any move, add or removal.
func stepNumbers(rec recipe.Recipe) map[recipe.StepID]int {
	out := map[recipe.StepID]int{}
	for _, c := range rec.Components {
		for _, s := range c.Steps {
			out[s.ID] = len(out) + 1
		}
	}
	return out
}

// dependsOn is s's dependencies as step numbers, in ascending order.
func dependsOn(s recipe.Step, numbers map[recipe.StepID]int) []int {
	var out []int
	for _, d := range s.DependsOn {
		out = append(out, numbers[d])
	}
	slices.Sort(out)
	return out
}

// confidence is the confidence a reviewed item goes back into the draft with:
// below the threshold if the form still flags it, else certain. The score
// itself is not kept through the form; only whether it was low.
func confidence(m mark, threshold float64) float64 {
	if m.Low {
		return threshold / 2
	}
	return 1
}

// carryMark puts a field's mark back: its confidence and where it came from.
func carryMark[T any](f *apiingest.Field[T], m mark, threshold float64) {
	f.Confidence, f.FromLLM = confidence(m, threshold), m.LLM
}
