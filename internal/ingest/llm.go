package ingest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Provider is the LLM provider this package consumes (ADR-00007): given the
// source and the gaps in the rule-based draft (ADR-00020), it returns the
// recipe in the ingestion schema. Each provider (Claude, Gemini) implements it in its own package with
// its own SDK; this package never imports one. Implementations apply the
// configured timeout and model, validate the response against the schema and
// return an error for an invalid one, never a partial Structured.
type Provider interface {
	Structure(ctx context.Context, req Request) (Structured, error)
}

// Request is what a provider is asked to structure.
type Request struct {
	// Text is the source text: pasted text, a page's main text or OCR output.
	Text string
	// Image is the source image for a vision-capable model, empty for text
	// input. ImageMIME is its media type.
	Image     []byte
	ImageMIME string
	// RuleDraft is the whole rule-based result the provider should improve on.
	// The fallback leaves it zero: it sends Gaps instead (ADR-00020 decision 2).
	RuleDraft Draft
	// Gaps is what the rules could not read well enough. When it is not Empty
	// the request is a gap-fill: the provider answers only for the gaps, and
	// the answer replaces exactly those and nothing else.
	Gaps Gaps
}

// Gaps names the parts of a rule-based draft that fell below the confidence
// threshold or are missing. It carries no value that met the threshold, so a
// provider is never shown, and cannot overwrite, a good rule-based reading.
//
// The answer is a Structured read this way:
//   - Name, Author, Description, Yield, PrepTime, CookTime and TotalTime are
//     read only for the scalar fields flagged here; the rest are ignored. Name
//     is always required by the schema, so a provider echoes it when it is not
//     a gap.
//   - Equipment is read only when the Equipment gap is set.
//   - When Components is set the draft has no ingredient lines, and the answer's
//     components are the recipe's, in full.
//   - StepGraph is read only for the steps listed in Steps; an entry for any
//     other step is ignored.
//   - Otherwise the answer's ingredient entries, read across its components in
//     order, are one replacement for each of Lines, in order. A different count
//     is an invalid answer.
type Gaps struct {
	Name, Author, Description, Yield, Prep, Cook, Total bool
	Equipment                                           bool
	Components                                          bool
	Lines                                               []GapLine
	// Steps is where step-graph gaps go: the dependencies and passive flag of
	// every step whose graph is the default (ADR-00020 decision 1). StepTexts is
	// every step of the recipe in reading order, so a dependency can name one;
	// it is set with Steps and carries no other part of the draft.
	Steps     []GapStep
	StepTexts []string
}

// GapLine is one ingredient line below the threshold. Component and Line
// locate it in the draft; Raw is the line as the source wrote it.
type GapLine struct {
	Component, Line int
	Raw             string
}

// GapStep is one step whose graph the rules left at the default. Component and
// Step locate it (both from 0); Number is its place in reading order across the
// whole recipe, from 1, which is how the answer names it.
type GapStep struct {
	Component, Step int
	Number          int
	Text            string
}

// Empty reports whether there is nothing to fill.
func (g Gaps) Empty() bool {
	scalars := g.Name || g.Author || g.Description || g.Yield || g.Prep || g.Cook || g.Total
	return !scalars && !g.Equipment && !g.Components && len(g.Lines) == 0 && len(g.Steps) == 0
}

// Describe renders the gaps as the plain text both providers put in the
// request, so the wording of a gap-fill lives in one place.
func (g Gaps) Describe() string {
	var b strings.Builder
	b.WriteString("Fill only these gaps in a rule-based reading of the source. Everything else in it is already correct and is not wanted.\n")
	for _, f := range []struct {
		on   bool
		name string
	}{
		{g.Name, "name"}, {g.Author, "author"}, {g.Description, "description"}, {g.Yield, "yield"},
		{g.Prep, "prep_time"}, {g.Cook, "cook_time"}, {g.Total, "total_time"},
	} {
		if f.on {
			fmt.Fprintf(&b, "- missing field: %s (leave it out if the source does not state it)\n", f.name)
		}
	}
	if g.Equipment {
		b.WriteString("- missing field: equipment (leave it out if the source names none)\n")
	}
	if g.Components {
		b.WriteString("- the reading found no ingredients: give the whole recipe in components\n")
	}
	if len(g.Lines) > 0 {
		b.WriteString("- ingredient lines the reading could not parse. Return one corrected entry for each, in this order, all in a single component, copied from the source:\n")
		for i, l := range g.Lines {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, l.Raw)
		}
	}
	if len(g.Steps) > 0 {
		b.WriteString("- step graph. The recipe's steps are numbered in reading order:\n")
		for i, text := range g.StepTexts {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, text)
		}
		nums := make([]string, len(g.Steps))
		for i, st := range g.Steps {
			nums[i] = strconv.Itoa(st.Number)
		}
		fmt.Fprintf(&b, "  Return one step_graph entry for each of steps %s, and none for the others: passive is true when the cook can leave the step unattended (baking, resting, simmering), and depends_on lists the numbers of earlier steps that must finish before it starts, empty when it can start at once. Steps that can run alongside each other do not depend on one another.\n", strings.Join(nums, ", "))
	}
	b.WriteString("The name field is always required: if the name is not listed as missing, repeat it as the source gives it.\n\n")
	return b.String()
}

// Structured is the ingestion schema a provider fills: the same raw shape the
// rule-based path reads, before ingredient lines are parsed. Time fields and
// ingredient lines are strings as written, so a provider answer goes through
// the same line parser and scoring as any other input.
type Structured struct {
	Name        string `json:"name"`
	Author      string `json:"author,omitempty"`
	Description string `json:"description,omitempty"`
	Yield       string `json:"yield,omitempty"`
	PrepTime    string `json:"prep_time,omitempty"`
	CookTime    string `json:"cook_time,omitempty"`
	TotalTime   string `json:"total_time,omitempty"`
	// Equipment names the equipment the recipe uses, as written ("Dutch
	// oven"); each is matched to the catalog like a source's own tool list.
	Equipment  []string              `json:"equipment,omitempty"`
	Components []StructuredComponent `json:"components"`
	// StepGraph answers the step-graph gap of a request: the passive flag and
	// dependencies of the steps it listed (ADR-00020 decision 1). Steps are
	// numbered from 1 in reading order across the whole recipe.
	StepGraph []StructuredStep `json:"step_graph,omitempty"`
}

// StructuredStep is the graph of one step in a provider's answer. DependsOn
// lists earlier step numbers and is empty, not absent, for a step that can
// start at once.
type StructuredStep struct {
	Step      int   `json:"step"`
	Passive   bool  `json:"passive"`
	DependsOn []int `json:"depends_on"`
}

// StructuredComponent is one named part of a provider's recipe; Name is empty
// for the unnamed component.
type StructuredComponent struct {
	Name        string   `json:"name,omitempty"`
	Ingredients []string `json:"ingredients"`
	Steps       []string `json:"steps"`
}
