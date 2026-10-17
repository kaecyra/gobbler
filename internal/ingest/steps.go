package ingest

import (
	"fmt"
	"regexp"
	"slices"
)

// passiveRE is the passive vocabulary (ADR-00020 decision 1): words that mark a
// step the cook can leave unattended. "baking" is left out on purpose, since it
// mostly names a dish or powder, and "rest" and "cool" count only where they
// are the instruction.
var passiveRE = regexp.MustCompile(`(?i)\b(bake[sd]?|roast(s|ed)?|proof|prove|marinate|chill|refrigerate|freeze|simmer|braise|ferment|steep|soak|set aside)\b` +
	`|(^|[.;!]\s+)(rest|rise|cool)\b` +
	`|\b(let|allow|leave)\b[^.;]*\b(rest|rise|stand|sit|cool|proof|prove|set)\b`)

// parallelRE is the parallel cue: a step that opens with, or follows a full stop
// with, wording that says it runs alongside the step before it. "while" counts
// only at the start, so "stir while the sauce thickens" does not break the chain.
var parallelRE = regexp.MustCompile(`(?i)(^|[.;!]\s+)(meanwhile|in the meantime|at the same time|while (the|it|they|you)\b)`)

// inferGraph sets the dependencies and passive flag of every step by rule
// (ADR-00020 decision 1). Steps chain in reading order, each after the one
// before it. A parallel step instead starts when the step before it starts, so
// it shares that step's dependencies, and the next step waits for every branch.
// A step a rule matched is marked GraphCue; the rest have the default graph.
// Numbers run from 1 across all components, as Step.DependsOn defines.
func inferGraph(comps []Component) {
	var tails, prev []int
	n := 0
	for ci := range comps {
		for si := range comps[ci].Steps {
			n++
			st := &comps[ci].Steps[si]
			passive, parallel := passiveRE.MatchString(st.Text), parallelRE.MatchString(st.Text)
			st.Passive, st.GraphCue, st.GraphFromLLM = passive, passive || parallel, false
			if parallel {
				st.DependsOn = slices.Clone(prev)
				tails = append(tails, n)
			} else {
				st.DependsOn = slices.Clone(tails)
				tails = []int{n}
			}
			prev = st.DependsOn
		}
	}
}

// stepCount is the number of steps across all components.
func stepCount(comps []Component) int {
	n := 0
	for _, c := range comps {
		n += len(c.Steps)
	}
	return n
}

// stepGaps lists the steps whose graph the rules left at the default, for the
// fallback to ask about, and the text of every step so a dependency can name
// one. A recipe with one step has no graph to infer, and a step whose graph the
// LLM already supplied is settled.
func stepGaps(comps []Component) ([]GapStep, []string) {
	if stepCount(comps) < 2 {
		return nil, nil
	}
	var gaps []GapStep
	var texts []string
	for ci, c := range comps {
		for si, st := range c.Steps {
			texts = append(texts, st.Text)
			if !st.GraphCue && !st.GraphFromLLM {
				gaps = append(gaps, GapStep{Component: ci, Step: si, Number: len(texts), Text: st.Text})
			}
		}
	}
	return gaps, texts
}

// applyStepGraph writes the provider's step graph onto the default steps gaps
// names, in comps, which the caller owns. An entry for any other step is
// ignored, and a gap step with no entry keeps the rules' graph. An entry that
// is out of range, depends on a step that is not earlier, or repeats another
// entry is an invalid answer.
func applyStepGraph(comps []Component, gaps []GapStep, answer []StructuredStep) error {
	total := stepCount(comps)
	byNumber := map[int]GapStep{}
	for _, g := range gaps {
		byNumber[g.Number] = g
	}
	seen := map[int]bool{}
	for _, a := range answer {
		if a.Step < 1 || a.Step > total {
			return fmt.Errorf("%w: step_graph names step %d of %d", ErrInvalidStructured, a.Step, total)
		}
		if seen[a.Step] {
			return fmt.Errorf("%w: step_graph answers step %d twice", ErrInvalidStructured, a.Step)
		}
		seen[a.Step] = true
		if err := checkDependsOn(a.Step, a.DependsOn); err != nil {
			return fmt.Errorf("%w: step_graph %w", ErrInvalidStructured, err)
		}
		g, ok := byNumber[a.Step]
		if !ok {
			continue
		}
		deps := slices.Clone(a.DependsOn)
		slices.Sort(deps)
		st := &comps[g.Component].Steps[g.Step]
		st.Passive, st.DependsOn, st.GraphFromLLM = a.Passive, slices.Compact(deps), true
	}
	return nil
}

// checkDependsOn is the rule an LLM answer must meet: every dependency is the
// number of an earlier step.
func checkDependsOn(step int, deps []int) error {
	for _, d := range deps {
		if d < 1 || d >= step {
			return fmt.Errorf("step %d depends on step %d, which is not an earlier step", step, d)
		}
	}
	return nil
}

// checkStepGraph reports the first step of comps with a dependency that is not
// a step of the draft, or is the step itself. Cycles are the store's to refuse
// (recipe.Recipe.ValidateSteps); a draft edited into one fails at Save.
func checkStepGraph(comps []Component) error {
	total, n := stepCount(comps), 0
	for _, c := range comps {
		for _, st := range c.Steps {
			n++
			for _, d := range st.DependsOn {
				if d < 1 || d > total || d == n {
					return fmt.Errorf("step %d (%q) depends on step %d, which is not another step of the recipe", n, st.Text, d)
				}
			}
		}
	}
	return nil
}
