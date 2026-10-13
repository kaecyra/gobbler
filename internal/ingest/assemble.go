package ingest

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/ingest/extract"
	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Issues the assembler reports on Draft.Issues, alongside the codes extract
// and parse report. Like theirs they are data about a less certain draft.
const (
	// IssueUnreadableYield: a yield was stated but could not be read.
	IssueUnreadableYield = "unreadable_yield"
	// IssueNoName: the source gave no recipe name.
	IssueNoName = "no_name"
	// IssueNoIngredients: no ingredient lines were found.
	IssueNoIngredients = "no_ingredients"
	// IssueNoSteps: no steps were found.
	IssueNoSteps = "no_steps"
)

// Confidences and penalties. Each penalty is subtracted from 1.
const (
	// textTitleConfidence is the confidence in a title taken from the first
	// lines of pasted text, which is a guess from layout.
	textTitleConfidence = 0.8
	// textNotesConfidence is the confidence in a description made from the
	// text that is neither ingredients nor steps.
	textNotesConfidence = 0.5

	penaltyNoName        = 0.3
	penaltyNoIngredients = 0.5
	penaltyNoSteps       = 0.3
)

var caloriesRE = regexp.MustCompile(`^\s*(\d+)`)

// assemble reads an extraction result into a scored Draft.
func (p *Pipeline) assemble(ctx context.Context, src Source, res extract.Result) (Draft, error) {
	rs := newResolver(ctx, p.deps.Catalog)
	d := Draft{
		ParserVersion: ParserVersion,
		Source:        src,
		Method:        res.Method,
		Threshold:     p.cfg.ConfidenceThreshold,
		Issues:        append([]string(nil), res.Issues...),
	}
	a := &assembler{rs: rs, d: &d}

	var structure float64
	switch {
	case res.Recipe != nil:
		a.fromRaw(*res.Recipe)
		structure = a.requiredStructure()
	case res.Method == extract.MethodText:
		sec, err := parse.SplitSections(res.Text)
		if err != nil {
			return Draft{}, fmt.Errorf("split recipe text: %w", err)
		}
		a.fromSections(sec)
		structure = sec.Confidence
	default:
		structure = a.requiredStructure()
	}
	if rs.err != nil {
		return Draft{}, fmt.Errorf("read catalog while drafting: %w", rs.err)
	}

	d.Proposed = ProposedFor(d.Components, nil)
	d.score(structure)
	return d, nil
}

type assembler struct {
	rs *resolver
	d  *Draft
}

func (a *assembler) fromRaw(raw extract.Raw) {
	d := a.d
	d.Name = stated(raw.Name, 1)
	d.Author = stated(raw.Author, 1)
	d.Description = stated(raw.Description, 1)
	a.yield(raw.Yield)
	d.Calories = calories(raw.Calories)
	d.Prep = duration(raw.PrepDuration)
	d.Cook = duration(raw.CookDuration)
	d.Total = duration(raw.TotalDuration)

	// A raw recipe lists ingredients once, so they go to the first,
	// unnamed component; steps that name a section get a component of their
	// own, in source order.
	comps := []Component{{}}
	byName := map[string]int{}
	for _, text := range raw.Ingredients {
		if l, ok := a.line(text); ok {
			comps[0].Lines = append(comps[0].Lines, l)
		}
	}
	for _, s := range raw.Instructions {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		i := 0
		if name := strings.TrimSpace(s.Section); name != "" {
			var ok bool
			if i, ok = byName[strings.ToLower(name)]; !ok {
				comps = append(comps, Component{Name: name})
				i = len(comps) - 1
				byName[strings.ToLower(name)] = i
			}
		}
		comps[i].Steps = append(comps[i].Steps, step(text, 1))
	}
	if len(comps) > 1 && len(comps[0].Lines) == 0 && len(comps[0].Steps) == 0 {
		comps = comps[1:]
	}
	if len(comps[0].Lines) > 0 || len(comps[0].Steps) > 0 || len(comps) > 1 {
		d.Components = comps
	}
}

func (a *assembler) fromSections(sec parse.Sections) {
	d := a.d
	d.Issues = append(d.Issues, sec.Issues...)
	d.Name = stated(sec.Title, textTitleConfidence)
	d.Description = stated(strings.Join(sec.Notes, "\n"), textNotesConfidence)
	for _, sc := range sec.Components {
		c := Component{Name: sc.Name}
		for _, raw := range sc.Ingredients {
			if l, ok := a.line(raw); ok {
				c.Lines = append(c.Lines, l)
			}
		}
		for _, s := range sc.Steps {
			if text := strings.TrimSpace(s); text != "" {
				// A heading-less split guesses which text is a step.
				c.Steps = append(c.Steps, step(text, sec.Confidence))
			}
		}
		d.Components = append(d.Components, c)
	}
}

// requiredStructure scores a draft by whether its required fields are present:
// a name, ingredient lines and steps. It records an issue for each missing one.
func (a *assembler) requiredStructure() float64 {
	d := a.d
	var lines, steps int
	for _, c := range d.Components {
		lines += len(c.Lines)
		steps += len(c.Steps)
	}
	s := 1.0
	if d.Name.Value == "" {
		s -= penaltyNoName
		d.Issues = append(d.Issues, IssueNoName)
	}
	if lines == 0 {
		s -= penaltyNoIngredients
		d.Issues = append(d.Issues, IssueNoIngredients)
	}
	if steps == 0 {
		s -= penaltyNoSteps
		d.Issues = append(d.Issues, IssueNoSteps)
	}
	return s
}

// line parses one ingredient line. A blank line is skipped, not reported.
func (a *assembler) line(raw string) (Line, bool) {
	if strings.TrimSpace(raw) == "" {
		return Line{}, false
	}
	r := parse.IngredientLine(raw, a.rs, a.rs)
	l := Line{
		Raw:        r.Raw,
		Name:       r.Name,
		Amount:     r.Amount,
		ToTaste:    r.ToTaste,
		Optional:   r.Optional,
		Confidence: r.Confidence,
		Issues:     r.Issues,
	}
	if r.Matched {
		l.Ingredient = recipe.IngredientID(r.Match.ID)
	}
	for _, q := range r.Qualifiers {
		kind := recipe.QualifierKind(q.Kind)
		if !kind.Valid() {
			kind = recipe.QualNote
		}
		l.Qualifiers = append(l.Qualifiers, recipe.Qualifier{Kind: kind, Text: q.Text})
	}
	return l, true
}

// ProposedFor lists what confirming comps would add to the catalog: the
// names of lines matched to no catalog ingredient, normalised, once each, in
// reading order. The assembler calls it with no prior list; the review screen
// calls it again after edits, passing the draft's earlier list so a name
// already proposed keeps its aisle.
func ProposedFor(comps []Component, prior []ProposedIngredient) []ProposedIngredient {
	aisle := make(map[string]string, len(prior))
	for _, p := range prior {
		aisle[p.Name] = p.Aisle
	}
	var out []ProposedIngredient
	seen := map[string]bool{}
	for _, c := range comps {
		for _, l := range c.Lines {
			name := normalizeName(l.Name)
			if l.Ingredient != 0 || name == "" || seen[name] {
				continue
			}
			seen[name] = true
			a, ok := aisle[name]
			if !ok {
				a = proposedAisle
			}
			out = append(out, ProposedIngredient{Name: name, Aisle: a})
		}
	}
	return out
}

// proposedIngredients is ProposedFor with no prior list, for the fallback path
// in fallback.go, which sits outside this node's lane; fold it into a direct
// ProposedFor call there.
func proposedIngredients(comps []Component) []ProposedIngredient {
	return ProposedFor(comps, nil)
}

// normalizeName is the catalog's stored form of a name: trimmed, lower-case,
// single spaces.
func normalizeName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// proposedAisle files a proposed ingredient until it is reviewed. It is the
// shopping list's own name for "no aisle" (see the test that pins them).
const proposedAisle = "uncategorised"

func stated(s string, confidence float64) Field[string] {
	s = strings.TrimSpace(s)
	if s == "" {
		return Field[string]{}
	}
	return Field[string]{Value: s, Confidence: confidence}
}

// duration reads a time field that extract has already parsed. A field that
// was stated but not read stays absent; extract reports it as an issue.
func duration(d time.Duration) Field[time.Duration] {
	if d <= 0 {
		return Field[time.Duration]{}
	}
	return Field[time.Duration]{Value: d, Confidence: 1}
}

// step builds a step, reading its duration from the text: the sum of every
// duration written in it, taking the upper bound of a range.
func step(text string, confidence float64) Step {
	var total time.Duration
	for _, f := range parse.FindDurations(text) {
		total += f.Max
	}
	return Step{Text: text, Duration: total, Confidence: confidence}
}

func calories(s string) Field[int] {
	m := caloriesRE.FindStringSubmatch(s)
	if m == nil {
		return Field[int]{}
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return Field[int]{}
	}
	return Field[int]{Value: n, Confidence: 1}
}

// yield reads the first yield text that starts with a quantity: "4" or
// "4 servings" give servings 4; "1 loaf" gives a yield of one loaf.
func (a *assembler) yield(texts []string) {
	var stated bool
	for _, t := range texts {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		stated = true
		qty, unit, ok := leadingQuantity(t)
		if !ok {
			continue
		}
		if unit == "" || strings.HasPrefix(strings.ToLower(unit), "serving") {
			a.d.Servings = Field[quantity.Quantity]{Value: qty, Confidence: 1}
			unit = "servings"
		}
		a.d.Yield = Field[recipe.Yield]{Value: recipe.Yield{Qty: qty, Unit: unit}, Confidence: 1}
		return
	}
	if stated {
		a.d.Issues = append(a.d.Issues, IssueUnreadableYield)
	}
}

// leadingQuantity splits "1 1/2 cups" into a quantity and the rest.
func leadingQuantity(s string) (quantity.Quantity, string, bool) {
	toks := strings.Fields(s)
	for n := min(3, len(toks)); n >= 1; n-- {
		if q, err := quantity.ParseQuantity(strings.Join(toks[:n], " ")); err == nil {
			return q, strings.Join(toks[n:], " "), true
		}
	}
	return quantity.Quantity{}, "", false
}
