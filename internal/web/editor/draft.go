package editor

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Form fields outside the indexed groups, and the structural actions a button
// can post in fieldAction. A group field is named by its position, so the
// field a user edited is always one the draft has: components "c.<c>.", lines
// "c.<c>.l.<l>.", qualifiers "c.<c>.l.<l>.q.<q>.", steps "c.<c>.s.<s>." and
// equipment "e.<e>.".
const (
	fieldVersion = "version"
	fieldAction  = "action"
	actionSave   = "save"
)

// draft is the editor form as text: every value is what the user typed, so a
// rejected save shows it back unchanged. Problems and the per-item Err strings
// are the messages of a rejected save or action.
type draft struct {
	Version int64

	Name, Description, SourceURL, Author string
	// Video is what was typed in the video field: a YouTube link or a bare ID.
	Video                         string
	YieldQty, YieldUnit, Servings string
	Calories, Tags                string
	Prep, Cook, Total             string

	Components []compDraft
	Equipment  []equipDraft

	// Errs holds the message under a metadata field, keyed by its form name.
	Errs map[string]string
	// Problems are messages about the recipe as a whole: a step cycle, a
	// stale save.
	Problems []string
}

type compDraft struct {
	Name  string
	Lines []lineDraft
	Steps []stepDraft
}

type lineDraft struct {
	Qty, Unit       string
	PkgQty, PkgUnit string
	Ingredient      string
	Propose         bool
	Optional        bool
	Rule            string
	Raw             string
	Quals           []qualDraft
	Err             string
	OfferProposal   bool // the ingredient matched nothing; offer to add it
}

type qualDraft struct{ Kind, Text string }

type stepDraft struct {
	ID       string
	Text     string
	Duration string
	Passive  bool
	Deps     []string
	// To is the component the step is in now, or is being moved to.
	To  int
	Err string
}

type equipDraft struct {
	Name          string
	Qty           string
	Steps         []string
	Propose       bool
	OfferProposal bool
	Err           string
}

// lookups are the catalog names the form suggests and the ids it labels.
type lookups struct {
	Ingredients, Equipment, Units []string
	ingredient, equipment         map[int64]string
}

// fromRecipe turns a stored recipe into the form's starting draft.
func fromRecipe(r recipe.Recipe, names lookups) draft {
	d := draft{
		Name: r.Name, Description: r.Description, SourceURL: r.SourceURL, Author: r.Author, Video: r.VideoID,
		YieldQty: qtyText(r.Yield.Qty), YieldUnit: r.Yield.Unit, Servings: qtyText(r.Servings),
		Tags: strings.Join(r.Tags, ", "),
		Prep: durationText(r.Times.Prep), Cook: durationText(r.Times.Cook), Total: durationText(r.Times.Total),
	}
	if r.CaloriesPerServing != nil {
		d.Calories = strconv.Itoa(*r.CaloriesPerServing)
	}
	for ci, c := range r.Components {
		cd := compDraft{Name: c.Name}
		for _, l := range c.Lines {
			ld := lineDraft{
				Qty: qtyText(l.Amount.Qty), Unit: l.Amount.Unit.Name,
				Ingredient: names.ingredient[int64(l.Ingredient)],
				Optional:   l.Optional, Rule: string(l.Rule), Raw: l.Raw,
			}
			if l.Amount.HasPackage {
				ld.PkgQty, ld.PkgUnit = qtyText(l.Amount.Package.Qty), l.Amount.Package.Unit.Name
			}
			for _, q := range l.Qualifiers {
				ld.Quals = append(ld.Quals, qualDraft{Kind: string(q.Kind), Text: q.Text})
			}
			cd.Lines = append(cd.Lines, ld)
		}
		for _, s := range c.Steps {
			cd.Steps = append(cd.Steps, stepDraft{
				ID: string(s.ID), Text: s.Text, Duration: durationText(s.Duration), Passive: s.Passive,
				Deps: stepIDs(s.DependsOn), To: ci,
			})
		}
		d.Components = append(d.Components, cd)
	}
	for _, e := range r.Equipment {
		d.Equipment = append(d.Equipment, equipDraft{
			Name: names.equipment[int64(e.Equipment)], Qty: qtyText(e.Qty), Steps: stepIDs(e.Steps),
		})
	}
	return d
}

func stepIDs(ids []recipe.StepID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = string(id)
	}
	return out
}

func qtyText(q quantity.Quantity) string { return q.String() }

// durationText writes d the way the editor reads it back: whole minutes as
// minutes, anything else in seconds. A zero duration is empty (unknown).
func durationText(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d%time.Minute == 0:
		return fmt.Sprintf("%d min", d/time.Minute)
	}
	return fmt.Sprintf("%d sec", (d+time.Second/2)/time.Second)
}

// parseForm reads every field of a posted editor form into a draft, in field
// order and renumbered from zero, so a hole in the posted indexes closes.
func parseForm(f url.Values) draft {
	d := draft{
		Name: f.Get("name"), Description: f.Get("description"), SourceURL: f.Get("source_url"), Author: f.Get("author"), Video: f.Get("video"),
		YieldQty: f.Get("yield_qty"), YieldUnit: f.Get("yield_unit"), Servings: f.Get("servings"),
		Calories: f.Get("calories"), Tags: f.Get("tags"),
		Prep: f.Get("prep"), Cook: f.Get("cook"), Total: f.Get("total"),
	}
	for _, ci := range indices(f, "c.") {
		cp := fmt.Sprintf("c.%d.", ci)
		c := compDraft{Name: f.Get(cp + "name")}
		for _, li := range indices(f, cp+"l.") {
			lp := fmt.Sprintf("%sl.%d.", cp, li)
			l := lineDraft{
				Qty: f.Get(lp + "qty"), Unit: f.Get(lp + "unit"),
				PkgQty: f.Get(lp + "pkgqty"), PkgUnit: f.Get(lp + "pkgunit"),
				Ingredient: f.Get(lp + "ing"), Propose: f.Get(lp+"propose") != "",
				Optional: f.Get(lp+"optional") != "", Rule: f.Get(lp + "rule"), Raw: f.Get(lp + "raw"),
			}
			for _, qi := range indices(f, lp+"q.") {
				qp := fmt.Sprintf("%sq.%d.", lp, qi)
				l.Quals = append(l.Quals, qualDraft{Kind: f.Get(qp + "kind"), Text: f.Get(qp + "text")})
			}
			c.Lines = append(c.Lines, l)
		}
		for _, si := range indices(f, cp+"s.") {
			sp := fmt.Sprintf("%ss.%d.", cp, si)
			to, err := strconv.Atoi(f.Get(sp + "to"))
			if err != nil {
				to = len(d.Components)
			}
			c.Steps = append(c.Steps, stepDraft{
				ID: strings.TrimSpace(f.Get(sp + "id")), Text: f.Get(sp + "text"), Duration: f.Get(sp + "dur"),
				Passive: f.Get(sp+"passive") != "", Deps: nonEmpty(f[sp+"dep"]), To: to,
			})
		}
		d.Components = append(d.Components, c)
	}
	for _, ei := range indices(f, "e.") {
		ep := fmt.Sprintf("e.%d.", ei)
		d.Equipment = append(d.Equipment, equipDraft{
			Name: f.Get(ep + "name"), Qty: f.Get(ep + "qty"), Steps: nonEmpty(f[ep+"step"]),
			Propose: f.Get(ep+"propose") != "",
		})
	}
	return d
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// indices returns the sorted distinct integers that follow prefix in the keys
// of f: for prefix "c.0.l." and the key "c.0.l.3.qty" it yields 3.
func indices(f url.Values, prefix string) []int {
	var out []int
	for k := range f {
		rest, ok := strings.CutPrefix(k, prefix)
		if !ok {
			continue
		}
		seg, _, _ := strings.Cut(rest, ".")
		n, err := strconv.Atoi(seg)
		if err != nil || n < 0 || slices.Contains(out, n) {
			continue
		}
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// arrange applies the moves the user asked for with a step's component
// selector, gives every step an id, and drops references to steps that are
// gone. It runs on every post, before any action.
func (d *draft) arrange() {
	type move struct{ from, pos, to int }
	var moves []move
	for ci, c := range d.Components {
		for si, s := range c.Steps {
			if s.To != ci && s.To >= 0 && s.To < len(d.Components) {
				moves = append(moves, move{ci, si, s.To})
			}
		}
	}
	// Collect first, then remove from the back so positions stay valid.
	var moved []struct {
		to   int
		step stepDraft
	}
	for i := len(moves) - 1; i >= 0; i-- {
		m := moves[i]
		c := &d.Components[m.from]
		moved = append(moved, struct {
			to   int
			step stepDraft
		}{m.to, c.Steps[m.pos]})
		c.Steps = slices.Delete(c.Steps, m.pos, m.pos+1)
	}
	for i := len(moved) - 1; i >= 0; i-- {
		d.Components[moved[i].to].Steps = append(d.Components[moved[i].to].Steps, moved[i].step)
	}
	d.settle()
}

// settle makes the draft self-consistent: every step has a unique id, its To
// is its position, and no dependency or equipment link names a missing step.
func (d *draft) settle() {
	used := map[string]bool{}
	for _, c := range d.Components {
		for _, s := range c.Steps {
			if s.ID != "" {
				used[s.ID] = true
			}
		}
	}
	next := 1
	for ci := range d.Components {
		c := &d.Components[ci]
		for si := range c.Steps {
			s := &c.Steps[si]
			s.To = ci
			if s.ID == "" {
				for used["s"+strconv.Itoa(next)] {
					next++
				}
				s.ID = "s" + strconv.Itoa(next)
				used[s.ID] = true
			}
		}
	}
	keep := func(ids []string) []string {
		var out []string
		for _, id := range ids {
			if used[id] && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		return out
	}
	for ci := range d.Components {
		for si := range d.Components[ci].Steps {
			s := &d.Components[ci].Steps[si]
			s.Deps = keep(s.Deps)
		}
	}
	for ei := range d.Equipment {
		d.Equipment[ei].Steps = keep(d.Equipment[ei].Steps)
	}
}

// apply performs one structural action ("add-line:0", "up-step:1.2", ...).
// The numbers are positions in the draft; an action that names something the
// draft does not have is an error, since the form only offers real ones.
func (d *draft) apply(action string) error {
	verb, arg, _ := strings.Cut(action, ":")
	pos, err := positions(arg)
	if err != nil {
		return fmt.Errorf("action %q: %w", action, err)
	}
	bad := fmt.Errorf("action %q names something that is not in the recipe", action)
	switch verb {
	case "add-component":
		d.Components = append(d.Components, compDraft{})
	case "rm-component", "up-component", "down-component":
		if len(pos) != 1 || !inRange(len(d.Components), pos[0]) {
			return bad
		}
		d.Components = reorder(d.Components, verb, pos[0])
	case "add-line":
		c, ok := d.component(pos, 1)
		if !ok {
			return bad
		}
		c.Lines = append(c.Lines, lineDraft{Rule: string(quantity.RuleLinear)})
	case "rm-line", "up-line", "down-line":
		c, ok := d.component(pos, 2)
		if !ok || !inRange(len(c.Lines), pos[1]) {
			return bad
		}
		c.Lines = reorder(c.Lines, verb, pos[1])
	case "add-qual":
		c, ok := d.component(pos, 2)
		if !ok || !inRange(len(c.Lines), pos[1]) {
			return bad
		}
		l := &c.Lines[pos[1]]
		l.Quals = append(l.Quals, qualDraft{Kind: string(recipe.QualPreparation)})
	case "rm-qual", "up-qual", "down-qual":
		c, ok := d.component(pos, 3)
		if !ok || !inRange(len(c.Lines), pos[1]) || !inRange(len(c.Lines[pos[1]].Quals), pos[2]) {
			return bad
		}
		l := &c.Lines[pos[1]]
		l.Quals = reorder(l.Quals, verb, pos[2])
	case "add-step":
		c, ok := d.component(pos, 1)
		if !ok {
			return bad
		}
		c.Steps = append(c.Steps, stepDraft{})
	case "rm-step", "up-step", "down-step":
		c, ok := d.component(pos, 2)
		if !ok || !inRange(len(c.Steps), pos[1]) {
			return bad
		}
		c.Steps = reorder(c.Steps, verb, pos[1])
	case "add-equip":
		d.Equipment = append(d.Equipment, equipDraft{})
	case "rm-equip", "up-equip", "down-equip":
		if len(pos) != 1 || !inRange(len(d.Equipment), pos[0]) {
			return bad
		}
		d.Equipment = reorder(d.Equipment, verb, pos[0])
	default:
		return fmt.Errorf("unknown action %q", action)
	}
	d.settle()
	return nil
}

// component returns the component addressed by pos[0] when pos has exactly n
// numbers.
func (d *draft) component(pos []int, n int) (*compDraft, bool) {
	if len(pos) != n || !inRange(len(d.Components), pos[0]) {
		return nil, false
	}
	return &d.Components[pos[0]], true
}

func positions(arg string) ([]int, error) {
	if arg == "" {
		return nil, nil
	}
	var out []int
	for _, p := range strings.Split(arg, ".") {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("position %q is not a number", p)
		}
		out = append(out, n)
	}
	return out, nil
}

func inRange(n, i int) bool { return i >= 0 && i < n }

// reorder removes (verb "rm-..."), or moves up or down by one, the item at i.
// An item that is already first or last stays where it is.
func reorder[T any](items []T, verb string, i int) []T {
	switch {
	case strings.HasPrefix(verb, "rm-"):
		return slices.Delete(items, i, i+1)
	case strings.HasPrefix(verb, "up-") && i > 0:
		items[i-1], items[i] = items[i], items[i-1]
	case strings.HasPrefix(verb, "down-") && i < len(items)-1:
		items[i+1], items[i] = items[i], items[i+1]
	}
	return items
}
