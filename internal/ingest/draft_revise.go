package ingest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// An agent may revise a draft held for review (ADR-00048): it sends the whole
// recipe as it should be, and the draft is replaced with it through the replace
// the review screen uses. What it changed is marked as the LLM fallback's
// values are marked (ADR-00006), so the person reviewing sees it; what it left
// alone keeps its marks. The source, parser version, method, scoring, picture
// choice and re-import marker are the held draft's and are never revised.

var (
	// ErrDraftChanged is returned (wrapped) for a revision made from a draft
	// that has changed since it was read, by a person on the review screen or
	// by another revision. The draft is left as it is.
	ErrDraftChanged = errors.New("the draft changed since it was read")
	// ErrInvalidRevision is returned (wrapped) for a revision that cannot be a
	// draft; the wrapped message says what is wrong with it.
	ErrInvalidRevision = errors.New("the revision is not valid")
)

// Revision is an agent's whole recipe for a held draft. Lines are text, read
// by the same line parser and catalog as any ingested line; the rest is values.
type Revision struct {
	Name, Author, Description string
	Yield                     recipe.Yield
	Servings                  quantity.Quantity
	Calories                  int
	Prep, Cook, Total         time.Duration
	MealType                  recipe.MealType
	Cuisine                   recipe.Cuisine
	Components                []RevisedComponent
	AuthorNotes               []RevisedNote
	Equipment                 []RevisedEquipment
}

// RevisedComponent is one component of a Revision.
type RevisedComponent struct {
	Name  string
	Lines []RevisedLine
	Steps []RevisedStep
}

// RevisedLine is one ingredient line: its text and, when the text names no
// ingredient the catalog has, the ingredient it is.
type RevisedLine struct {
	Text       string
	Ingredient recipe.IngredientID
	Optional   bool
}

// RevisedStep is one step. DependsOn uses the revision's own step numbers,
// from 1 in reading order across every component, as Step does.
type RevisedStep struct {
	Text, Title string
	Kind        recipe.StageKind
	Duration    time.Duration
	Passive     bool
	DependsOn   []int
}

// RevisedNote is one of the author's notes.
type RevisedNote struct{ Title, Text string }

// RevisedEquipment is one piece of equipment: its name, and the catalog entry
// it was listed with when it is one the draft already has.
type RevisedEquipment struct {
	Name      string
	Equipment recipe.EquipmentID
}

// Revise returns held with r in place of its recipe. A field, line, step or
// piece of equipment whose value differs from held's is marked FromLLM; a
// step whose passive flag or dependencies differ has GraphFromLLM and no other
// graph mark. One equal to held's is held's own, with its marks, confidence
// and parsed reading, wherever it now sits. held is not modified. A revision
// that cannot be a draft is ErrInvalidRevision.
func Revise(ctx context.Context, cat CatalogReader, held Draft, r Revision) (Draft, error) {
	out := held
	a := &assembler{rs: newResolver(ctx, cat), d: &out}
	name := strings.TrimSpace(r.Name)
	switch {
	case name == "":
		return Draft{}, fmt.Errorf("%w: a recipe needs a name", ErrInvalidRevision)
	case len(r.Components) == 0:
		return Draft{}, fmt.Errorf("%w: a recipe needs at least one component", ErrInvalidRevision)
	case r.MealType != "" && !r.MealType.Valid():
		return Draft{}, fmt.Errorf("%w: %q is not a meal type", ErrInvalidRevision, r.MealType)
	case r.Cuisine != "" && !r.Cuisine.Valid():
		return Draft{}, fmt.Errorf("%w: %q is not a cuisine", ErrInvalidRevision, r.Cuisine)
	case r.Calories < 0 || r.Prep < 0 || r.Cook < 0 || r.Total < 0:
		return Draft{}, fmt.Errorf("%w: calories and times cannot be negative", ErrInvalidRevision)
	}
	same := func(a, b string) bool { return a == b }
	out.Name = revisedField(held.Name, name, same, false)
	out.Author = revisedField(held.Author, strings.TrimSpace(r.Author), same, strings.TrimSpace(r.Author) == "")
	out.Description = revisedField(held.Description, strings.TrimSpace(r.Description), same, strings.TrimSpace(r.Description) == "")
	out.Yield = revisedField(held.Yield, r.Yield, func(a, b recipe.Yield) bool {
		return a.Unit == b.Unit && a.Qty.String() == b.Qty.String()
	}, r.Yield.Unit == "" && r.Yield.Qty.IsAbsent())
	out.Servings = revisedField(held.Servings, r.Servings, func(a, b quantity.Quantity) bool { return a.String() == b.String() }, r.Servings.IsAbsent())
	out.Calories = revisedField(held.Calories, r.Calories, func(a, b int) bool { return a == b }, r.Calories == 0)
	sameDur := func(a, b time.Duration) bool { return a == b }
	out.Prep = revisedField(held.Prep, r.Prep, sameDur, r.Prep == 0)
	out.Cook = revisedField(held.Cook, r.Cook, sameDur, r.Cook == 0)
	out.Total = revisedField(held.Total, r.Total, sameDur, r.Total == 0)
	if r.MealType != held.MealType {
		out.MealType, out.MealTypeFromLLM = r.MealType, true
	}
	if r.Cuisine != held.Cuisine {
		out.Cuisine, out.CuisineFromLLM = r.Cuisine, true
	}

	comps, err := reviseComponents(a, held.Components, r.Components)
	if err != nil {
		return Draft{}, err
	}
	out.Components = comps
	out.AuthorNotes = reviseNotes(held.AuthorNotes, r.AuthorNotes)
	if out.Equipment, err = reviseEquipment(a, held.Equipment, r.Equipment); err != nil {
		return Draft{}, err
	}
	if a.rs.err != nil {
		return Draft{}, fmt.Errorf("read catalog while revising: %w", a.rs.err)
	}
	out.Proposed = ProposedFor(out.Components, held.Proposed)
	return out, nil
}

// revisedField is held when v is held's value, and otherwise v marked as
// supplied by the LLM, as the fallback marks it. A field the agent cleared is
// marked too, with no value, so the reviewer sees what was removed.
func revisedField[T any](held Field[T], v T, same func(a, b T) bool, empty bool) Field[T] {
	switch {
	case same(held.Value, v):
		return held
	case empty:
		var zero T
		return Field[T]{Value: zero, Confidence: llmFieldConfidence, FromLLM: true}
	}
	return Field[T]{Value: v, Confidence: llmFieldConfidence, FromLLM: true}
}

// reviseComponents builds the revision's components. A line or step is matched
// to an unused held one by its text, in reading order, so one that moved is
// still held's own.
func reviseComponents(a *assembler, held []Component, in []RevisedComponent) ([]Component, error) {
	var heldLines []Line
	var heldSteps []Step
	heldComp := map[string]Component{}
	for _, c := range held {
		heldLines = append(heldLines, c.Lines...)
		heldSteps = append(heldSteps, c.Steps...)
		if _, ok := heldComp[c.Name]; !ok {
			heldComp[c.Name] = c
		}
	}
	usedLine := make([]bool, len(heldLines))
	usedStep := make([]bool, len(heldSteps))

	// matched[n-1] is the held step number (from 1) the revision's step n was
	// matched to, or 0.
	var matched []int
	var out []Component
	n := 0
	for ci, c := range in {
		name := strings.TrimSpace(c.Name)
		comp := Component{Name: name, FromLLM: true}
		if h, ok := heldComp[name]; ok {
			comp.FromLLM = h.FromLLM
		}
		for li, l := range c.Lines {
			line, err := reviseLine(a, heldLines, usedLine, l)
			if err != nil {
				return nil, fmt.Errorf("%w: component %d, ingredient %d: %w", ErrInvalidRevision, ci+1, li+1, err)
			}
			comp.Lines = append(comp.Lines, line)
		}
		for si, s := range c.Steps {
			n++
			where := fmt.Sprintf("component %d, step %d", ci+1, si+1)
			text, title := strings.TrimSpace(s.Text), strings.TrimSpace(s.Title)
			switch {
			case text == "":
				return nil, fmt.Errorf("%w: %s has no text", ErrInvalidRevision, where)
			case s.Kind != "" && !s.Kind.Valid():
				return nil, fmt.Errorf("%w: %s: %q is not a stage kind", ErrInvalidRevision, where, s.Kind)
			case s.Duration < 0:
				return nil, fmt.Errorf("%w: %s: its time cannot be negative", ErrInvalidRevision, where)
			}
			at := 0
			st := Step{Text: text, Title: title, Kind: s.Kind, Duration: s.Duration, Confidence: llmFieldConfidence, FromLLM: true}
			for hi, h := range heldSteps {
				if usedStep[hi] || h.Text != text {
					continue
				}
				usedStep[hi], at = true, hi+1
				if h.Title == title && h.Kind == s.Kind && h.Duration == s.Duration {
					st = h
				} else {
					st.Confidence = h.Confidence
				}
				break
			}
			matched = append(matched, at)
			st.Passive, st.DependsOn = s.Passive, sortedSteps(s.DependsOn)
			comp.Steps = append(comp.Steps, st)
		}
		out = append(out, comp)
	}

	if err := checkStepGraph(out); err != nil {
		return nil, fmt.Errorf("%w: %w (steps are numbered from 1 in reading order across every component)", ErrInvalidRevision, err)
	}

	// A held step's dependencies, renumbered to the revision's numbers, are
	// what the revision's step must have for its graph to be held's.
	renumber := map[int]int{}
	for i, at := range matched {
		if at != 0 {
			renumber[at] = i + 1
		}
	}
	n = 0
	for ci := range out {
		for si := range out[ci].Steps {
			st := &out[ci].Steps[si]
			at := matched[n]
			n++
			if at != 0 {
				h := heldSteps[at-1]
				var was []int
				for _, d := range h.DependsOn {
					if nd, ok := renumber[d]; ok {
						was = append(was, nd)
					} else {
						was = append(was, -1) // a step the revision dropped
					}
				}
				if h.Passive == st.Passive && slices.Equal(sortedSteps(was), st.DependsOn) {
					st.GraphCue, st.GraphFromLLM, st.GraphByUser = h.GraphCue, h.GraphFromLLM, h.GraphByUser
					continue
				}
			}
			st.GraphCue, st.GraphFromLLM, st.GraphByUser = false, true, false
		}
	}
	return out, nil
}

func sortedSteps(in []int) []int {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

// reviseLine is the unused held line with l's text, optional flag and
// ingredient, or else l read afresh by the line parser and marked FromLLM.
func reviseLine(a *assembler, held []Line, used []bool, l RevisedLine) (Line, error) {
	text := strings.Join(strings.Fields(l.Text), " ")
	if text == "" {
		return Line{}, errors.New("the line has no text")
	}
	for i, h := range held {
		if !used[i] && strings.Join(strings.Fields(h.Raw), " ") == text && h.Optional == l.Optional && (l.Ingredient == 0 || l.Ingredient == h.Ingredient) {
			used[i] = true
			return h, nil
		}
	}
	line, _ := a.line(text)
	if l.Ingredient != 0 {
		line.Ingredient = l.Ingredient
	}
	line.Optional = line.Optional || l.Optional
	line.FromLLM = true
	return line, nil
}

// reviseNotes keeps a held note the revision repeats, with its confidence,
// issues and mark; a new or changed note is the agent's, marked FromLLM.
func reviseNotes(held []AuthorNote, in []RevisedNote) []AuthorNote {
	used := make([]bool, len(held))
	var out []AuthorNote
	for _, n := range in {
		title, text := strings.TrimSpace(n.Title), strings.TrimSpace(n.Text)
		if text == "" {
			continue
		}
		note := AuthorNote{Title: title, Text: text, Confidence: llmFieldConfidence, FromLLM: true}
		for i, h := range held {
			if !used[i] && h.Title == title && h.Text == text {
				used[i], note = true, h
				break
			}
		}
		out = append(out, note)
	}
	return out
}

// reviseEquipment keeps held equipment the revision lists, by catalog entry or
// name; a new name is matched against the catalog as an LLM answer's is, and
// marked FromLLM.
func reviseEquipment(a *assembler, held []Equipment, in []RevisedEquipment) ([]Equipment, error) {
	set := newEquipmentSet()
	for i, e := range in {
		name := equipmentName(e.Name)
		kept := false
		for _, h := range held {
			if (e.Equipment != 0 && e.Equipment == h.Equipment) || (e.Equipment == 0 && name == h.Name) {
				set.add(h)
				kept = true
				break
			}
		}
		switch {
		case kept:
		case e.Equipment != 0:
			return nil, fmt.Errorf("%w: equipment %d: %d is not equipment of this draft; give a new piece by name alone", ErrInvalidRevision, i+1, e.Equipment)
		case name == "":
			return nil, fmt.Errorf("%w: equipment %d: %q is not a name of equipment", ErrInvalidRevision, i+1, e.Name)
		default:
			a.named(set, name, true)
		}
	}
	return set.items, nil
}
