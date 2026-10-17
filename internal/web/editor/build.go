package editor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/video"
)

// proposal is a catalog entry the user asked to add as unreviewed. It sits at
// a place in the built recipe whose id is filled in once the entry exists.
type proposal struct {
	name      string
	equipment bool
	// at locates the place: a line (component, line) or equipment (index).
	component, index int
}

// build turns the draft into a recipe on top of base, which supplies what the
// form does not edit (the rating and the special times). Anything the user
// typed that cannot be used is written into d as a message, and ok is false;
// the recipe is then not to be saved. err is a failure to read the catalog.
// Proposals name the ingredients and equipment to create before saving.
func (h *handler) build(ctx context.Context, d *draft, base recipe.Recipe) (rec recipe.Recipe, props []proposal, ok bool, err error) {
	d.Errs, d.Problems = map[string]string{}, nil
	b := builder{ctx: ctx, cat: h.Catalog, d: d}
	rec = base
	rec.Name = strings.TrimSpace(d.Name)
	if rec.Name == "" {
		d.Errs["name"] = "A recipe needs a name."
	}
	rec.Description, rec.SourceURL, rec.Author = d.Description, strings.TrimSpace(d.SourceURL), strings.TrimSpace(d.Author)
	rec.VideoID = b.video()
	rec.Tags = tagList(d.Tags)
	rec.Yield.Unit = strings.TrimSpace(d.YieldUnit)
	rec.Yield.Qty = b.optionalQuantity("yield_qty", d.YieldQty, "The yield amount")
	rec.Servings = b.optionalQuantity("servings", d.Servings, "Servings")
	rec.CaloriesPerServing = b.calories()
	rec.Times.Prep = b.duration("prep", d.Prep, "Prep time")
	rec.Times.Cook = b.duration("cook", d.Cook, "Cook time")
	rec.Times.Total = b.duration("total", d.Total, "Total time")

	rec.Components = nil
	for ci := range d.Components {
		c := &d.Components[ci]
		comp := recipe.Component{Name: strings.TrimSpace(c.Name)}
		for li := range c.Lines {
			line, keep, prop := b.line(&c.Lines[li])
			if !keep {
				continue
			}
			if prop != "" {
				props = append(props, proposal{name: prop, component: ci, index: len(comp.Lines)})
			}
			comp.Lines = append(comp.Lines, line)
		}
		for si := range c.Steps {
			if step, keep := b.step(&c.Steps[si]); keep {
				comp.Steps = append(comp.Steps, step)
			}
		}
		rec.Components = append(rec.Components, comp)
	}
	rec.Equipment = nil
	for ei := range d.Equipment {
		link, keep, prop := b.equipment(&d.Equipment[ei])
		if !keep {
			continue
		}
		if prop != "" {
			props = append(props, proposal{name: prop, equipment: true, index: len(rec.Equipment)})
		}
		rec.Equipment = append(rec.Equipment, link)
	}
	if b.err != nil {
		return recipe.Recipe{}, nil, false, b.err
	}
	if b.failed || len(d.Errs) > 0 {
		return recipe.Recipe{}, nil, false, nil
	}
	if err := rec.Validate(); err != nil {
		d.Problems = append(d.Problems, validationMessage(err))
		return recipe.Recipe{}, nil, false, nil
	}
	return rec, props, true, nil
}

// validationMessage words a structural error of the recipe for the form. A
// step cycle already names its steps.
func validationMessage(err error) string {
	switch {
	case errors.Is(err, recipe.ErrCycle), errors.Is(err, recipe.ErrUnknownDependency), errors.Is(err, recipe.ErrBadStepID):
		var ce *recipe.CycleError
		if errors.As(err, &ce) {
			return "These steps depend on each other in a circle, so no order can satisfy them. " + ce.Error() + "."
		}
		return err.Error() + "."
	case errors.Is(err, recipe.ErrNoComponents):
		return "A recipe needs at least one component. Add one before saving."
	}
	return err.Error() + "."
}

// propose creates the unreviewed catalog entries the user asked for and puts
// their ids into rec. It runs after every check of the form has passed, so a
// form rejected by validation or found stale beforehand adds nothing. The
// entries are separate from the recipe write, as with ingest.Saver.Save: if a
// later proposal or the recipe write fails (a store error, or a change landing
// after the version check), the entries already created stay, unreviewed, for
// the catalog page.
func (h *handler) propose(ctx context.Context, rec *recipe.Recipe, props []proposal) error {
	made := map[string]int64{}
	for _, p := range props {
		key := fmt.Sprintf("%t/%s", p.equipment, strings.Join(strings.Fields(strings.ToLower(p.name)), " "))
		id, done := made[key]
		if !done {
			if p.equipment {
				e, err := h.Catalog.CreateEquipment(ctx, catalog.Equipment{Name: p.name, Reviewed: false})
				if err != nil {
					return fmt.Errorf("add equipment %q to the catalog: %w", p.name, err)
				}
				id = e.ID
			} else {
				i, err := h.Catalog.CreateIngredient(ctx, catalog.Ingredient{Name: p.name, Aisle: shopping.Uncategorised, Reviewed: false})
				if err != nil {
					return fmt.Errorf("add ingredient %q to the catalog: %w", p.name, err)
				}
				id = i.ID
			}
			made[key] = id
		}
		if p.equipment {
			rec.Equipment[p.index].Equipment = recipe.EquipmentID(id)
		} else {
			rec.Components[p.component].Lines[p.index].Ingredient = recipe.IngredientID(id)
		}
	}
	return nil
}

// builder reads the draft's text into model values, recording a message
// beside each thing it cannot read. Catalog lookups are remembered for the
// request.
type builder struct {
	ctx    context.Context
	cat    Catalog
	d      *draft
	failed bool
	err    error

	units map[string]unitHit
}

type unitHit struct {
	u  quantity.Unit
	ok bool
}

func (b *builder) optionalQuantity(field, text, label string) quantity.Quantity {
	text = strings.TrimSpace(text)
	if text == "" {
		return quantity.Absent()
	}
	q, err := quantity.ParseQuantity(text)
	if err != nil {
		b.d.Errs[field] = quantityMessage(label, text)
		return quantity.Absent()
	}
	return q
}

func quantityMessage(label, text string) string {
	return fmt.Sprintf("%s %q is not a quantity. Use a whole number, decimal, fraction or mixed number such as 1 1/2, ½ or 0.75, or a range such as 2-3.", label, text)
}

// video reads the video field into the 11-character ID the recipe stores. A
// link or ID that cannot be read is a message beside the field and an empty ID;
// the recipe is then not saved.
func (b *builder) video() string {
	id, err := video.ParseID(b.d.Video)
	if err != nil {
		b.d.Errs["video"] = fmt.Sprintf("%q is not a YouTube video. %s", strings.TrimSpace(b.d.Video), video.Message)
		return ""
	}
	return id
}

func (b *builder) calories() *int {
	text := strings.TrimSpace(b.d.Calories)
	if text == "" {
		return nil
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 0 {
		b.d.Errs["calories"] = fmt.Sprintf("Calories %q must be a whole number of calories, or empty if unknown.", text)
		return nil
	}
	return &n
}

// duration reads a time field. Empty is zero (unknown). A range, or text
// the parser read only in part, is refused rather than guessed at.
func (b *builder) duration(field, text, label string) time.Duration {
	d, msg := readDuration(text)
	if msg != "" {
		b.d.Errs[field] = label + ": " + msg
	}
	return d
}

func readDuration(text string) (time.Duration, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, ""
	}
	r := parse.Duration(text)
	switch {
	case !r.Found:
		return 0, fmt.Sprintf("%q is not a duration. Write it like 45 min, 1 h 30 min or 2 hours.", text)
	case r.Range:
		return 0, fmt.Sprintf("%q is a range. Enter one duration.", text)
	case r.Confidence < 1:
		return 0, fmt.Sprintf("%q holds more than a duration. Write it like 45 min or 1 h 30 min.", text)
	}
	return r.Min, ""
}

func (b *builder) unit(term string) (quantity.Unit, bool) {
	term = strings.TrimSpace(term)
	if term == "" {
		return quantity.Unit{}, true
	}
	if hit, seen := b.units[term]; seen {
		return hit.u, hit.ok
	}
	cu, ok, err := b.cat.MatchUnit(b.ctx, term)
	if err != nil {
		b.err = errors.Join(b.err, fmt.Errorf("match unit %q: %w", term, err))
		return quantity.Unit{}, false
	}
	if b.units == nil {
		b.units = map[string]unitHit{}
	}
	b.units[term] = unitHit{cu.Unit, ok}
	return cu.Unit, ok
}

// line reads one ingredient line. keep is false for a line left entirely
// blank, which is dropped. prop is the name of an ingredient to add as
// unreviewed, or "".
func (b *builder) line(l *lineDraft) (line recipe.IngredientLine, keep bool, prop string) {
	l.Err, l.OfferProposal = "", false
	if lineBlank(*l) {
		return line, false, ""
	}
	var msgs []string
	say := func(format string, args ...any) { msgs = append(msgs, fmt.Sprintf(format, args...)) }

	if text := strings.TrimSpace(l.Qty); text != "" {
		q, err := quantity.ParseQuantity(text)
		if err != nil {
			say("%s", quantityMessage("Quantity", text))
		}
		line.Amount.Qty = q
	}
	if u, ok := b.unit(l.Unit); !ok {
		say("There is no unit called %q. Use a unit from the suggestions, or leave it empty for a count such as 2 eggs.", strings.TrimSpace(l.Unit))
	} else {
		line.Amount.Unit = u
	}
	if pq, pu := strings.TrimSpace(l.PkgQty), strings.TrimSpace(l.PkgUnit); pq != "" || pu != "" {
		q, err := quantity.ParseQuantity(pq)
		u, ok := b.unit(pu)
		switch {
		case pq == "" || pu == "":
			say("Package size needs both an amount and a unit, or neither.")
		case err != nil:
			say("%s", quantityMessage("Package amount", pq))
		case !ok:
			say("There is no unit called %q for the package size.", pu)
		default:
			line.Amount.Package, line.Amount.HasPackage = quantity.PackageSize{Qty: q, Unit: u}, true
		}
	}

	name := strings.TrimSpace(l.Ingredient)
	switch {
	case name == "" && strings.TrimSpace(l.Raw) == "":
		say("Choose an ingredient.")
	case name != "":
		ing, found, err := b.cat.MatchIngredient(b.ctx, name)
		switch {
		case err != nil:
			b.err = errors.Join(b.err, fmt.Errorf("match ingredient %q: %w", name, err))
		case found:
			line.Ingredient = recipe.IngredientID(ing.ID)
		case l.Propose:
			prop = name
		default:
			l.OfferProposal = true
			say("No ingredient in the catalog is called %q. Pick one from the suggestions, or tick the box to add it as a new, unreviewed ingredient.", name)
		}
	}

	rule, err := quantity.ParseRule(strings.TrimSpace(l.Rule))
	if err != nil {
		say("Scaling rule %q must be linear, fixed or to_taste.", l.Rule)
	}
	line.Rule, line.Optional, line.Raw = rule, l.Optional, strings.TrimSpace(l.Raw)

	for _, q := range l.Quals {
		text := strings.TrimSpace(q.Text)
		if text == "" {
			continue
		}
		kind := recipe.QualifierKind(q.Kind)
		if !kind.Valid() {
			say("Qualifier kind %q must be preparation, state, form or note.", q.Kind)
			continue
		}
		line.Qualifiers = append(line.Qualifiers, recipe.Qualifier{Kind: kind, Text: text})
	}
	if len(msgs) > 0 {
		b.failed = true
		l.Err = strings.Join(msgs, " ")
		return line, true, ""
	}
	return line, true, prop
}

func lineBlank(l lineDraft) bool {
	for _, s := range []string{l.Qty, l.Unit, l.PkgQty, l.PkgUnit, l.Ingredient, l.Raw} {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	for _, q := range l.Quals {
		if strings.TrimSpace(q.Text) != "" {
			return false
		}
	}
	return true
}

// step reads one step; a step with no text, duration or dependencies is
// dropped as an unused row.
func (b *builder) step(s *stepDraft) (recipe.Step, bool) {
	s.Err = ""
	text := strings.TrimSpace(s.Text)
	if text == "" && strings.TrimSpace(s.Duration) == "" && len(s.Deps) == 0 {
		return recipe.Step{}, false
	}
	step := recipe.Step{ID: recipe.StepID(s.ID), Text: text, Passive: s.Passive}
	var msgs []string
	if text == "" {
		msgs = append(msgs, "A step needs text.")
	}
	dur, msg := readDuration(s.Duration)
	if msg != "" {
		msgs = append(msgs, "Duration: "+msg)
	}
	step.Duration = dur
	for _, dep := range s.Deps {
		step.DependsOn = append(step.DependsOn, recipe.StepID(dep))
	}
	if len(msgs) > 0 {
		b.failed = true
		s.Err = strings.Join(msgs, " ")
	}
	return step, true
}

func (b *builder) equipment(e *equipDraft) (link recipe.EquipmentLink, keep bool, prop string) {
	e.Err, e.OfferProposal = "", false
	name, qty := strings.TrimSpace(e.Name), strings.TrimSpace(e.Qty)
	if name == "" && qty == "" && len(e.Steps) == 0 {
		return link, false, ""
	}
	var msgs []string
	if qty != "" {
		q, err := quantity.ParseQuantity(qty)
		if err != nil {
			msgs = append(msgs, quantityMessage("Equipment quantity", qty))
		}
		link.Qty = q
	}
	if name == "" {
		msgs = append(msgs, "Choose the equipment.")
	} else {
		eq, found, err := b.cat.MatchEquipment(b.ctx, name)
		switch {
		case err != nil:
			b.err = errors.Join(b.err, fmt.Errorf("match equipment %q: %w", name, err))
		case found:
			link.Equipment = recipe.EquipmentID(eq.ID)
		case e.Propose:
			prop = name
		default:
			e.OfferProposal = true
			msgs = append(msgs, fmt.Sprintf("No equipment in the catalog is called %q. Pick one from the suggestions, or tick the box to add it as new, unreviewed equipment.", name))
		}
	}
	for _, id := range e.Steps {
		link.Steps = append(link.Steps, recipe.StepID(id))
	}
	if len(msgs) > 0 {
		b.failed = true
		e.Err = strings.Join(msgs, " ")
		return link, true, ""
	}
	return link, true, prop
}

// tagList splits a comma-separated tag field into trimmed, distinct tags.
func tagList(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}
