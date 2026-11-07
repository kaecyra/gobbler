package ingest

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/kaecyra/gobbler/internal/parse"
	"github.com/kaecyra/gobbler/internal/recipe"
)

const passiveVerbs = `(bake|roast|proof|prove|marinate|chill|refrigerate|freeze|simmer|braise|ferment|steep|soak)`

// The passive vocabulary (ADR-00020 decision 1): words that mark a step the
// cook can leave unattended. Each form matches only where it is the
// instruction, never as an adjective, an ingredient or part of another phrase,
// because a false positive hides a step from the cook's attention.
var (
	// passiveVerbRE matches the base form of a passive verb where an
	// instruction can start. At the start of a step or sentence any following
	// word is fine. After a comma, "and", "then" or "to" the verb must be
	// followed by what a verb takes (an article or pronoun, "for", "until", a
	// number) or end its clause, because there a noun is as likely: "add the
	// carrots and roast beef". A single adverb may precede it ("gently
	// simmer"). "roasted peppers", "baked beans" and "freeze-dried" are not
	// instructions. The -s and -ing forms are left out on purpose: "while it
	// simmers" inside an active step must not make that step passive.
	passiveVerbRE = verbRE(passiveVerbs)
	// passiveStartRE matches "rest", "rise" and "cool" only as the instruction
	// that opens a sentence.
	passiveStartRE = regexp.MustCompile(`(?i)(^|[.;!]\s+)(rest|rise|cool)\b`)
	// passiveLetRE matches "let the dough rise", "leave to rest": a few plain
	// words between the verb and the waiting word, which must end its phrase.
	// "set the timer", "the rest of the flour" and "stand mixer" fail that.
	passiveLetRE  = regexp.MustCompile(`(?i)\b(let|allow|leave)\s+(\w+\s+){0,4}?(rest|rise|stand|sit|cool|set|proof|prove)(\s+(for|until|overnight|at|in|on|to|about|before|completely|slightly|fully|briefly|thoroughly|\d)|[.,;:!]|$)`)
	clauseSplitRE = regexp.MustCompile(`(?i)\b(and|then)\b`)
)

// verbRE builds the pattern passiveVerbRE describes for any alternation of
// base-form verbs, so the stage kind vocabulary reads an instruction exactly as
// the passive vocabulary does.
func verbRE(verbs string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(^|[.;:!]\s+)(\w+ly\s+)?` + verbs + `(\s|[.,;:!]|$)` +
		`|(,\s+|\b(and|then|to)\s+)(\w+ly\s+)?` + verbs + `(\s+(the|it|them|this|these|for|until|overnight|at|in|on|to|about|\d+)\b|[.,;:!]|$)`)
}

// isPassive reports whether text instructs the cook to leave something to
// work unattended. A "let ..." phrase does not count if it runs on into
// another clause ("let it cool and ..." is read from its own words only).
func isPassive(text string) bool {
	if passiveVerbRE.MatchString(text) || passiveStartRE.MatchString(text) {
		return true
	}
	for _, m := range passiveLetRE.FindAllString(text, -1) {
		if !clauseSplitRE.MatchString(m) {
			return true
		}
	}
	return false
}

// parallelRE is the parallel cue: a step that opens with wording that says it
// runs alongside the step before it. It counts only at the start of the step,
// since a cue after a full stop leaves the first sentence in sequence, and
// "stir while the sauce thickens" is not parallel to anything.
var parallelRE = regexp.MustCompile(`(?i)^\s*(meanwhile|in the meantime|at the same time|while (the|it|they|you)\b)`)

// inferGraph sets the dependencies and passive flag of every step by rule
// (ADR-00020 decision 1). Steps chain in reading order, each after the one
// before it. A parallel step instead starts when the step before it starts, so
// it shares that step's dependencies, and the next step waits for every branch.
// A step a rule matched is marked GraphCue; the rest have the default graph.
// Numbers run from 1 across all components, as Step.DependsOn defines. A step
// the user set (GraphByUser) keeps its graph, whose numbers the caller must
// translate before this runs if the steps were renumbered (see
// renumberUserGraphs), since a parallel step after it copies its dependencies.
func inferGraph(comps []Component) {
	var tails, prev []int
	n := 0
	for ci := range comps {
		for si := range comps[ci].Steps {
			n++
			st := &comps[ci].Steps[si]
			if st.GraphByUser {
				// The user's graph stands. Its step still counts as a link in
				// the chain for the steps after it.
				tails, prev = []int{n}, st.DependsOn
				continue
			}
			passive, parallel := isPassive(st.Text), parallelRE.MatchString(st.Text)
			st.Passive, st.GraphCue, st.GraphFromLLM, st.GraphByUser = passive, passive || parallel, false, false
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
// LLM supplied or the user set is settled.
func stepGaps(comps []Component) ([]GapStep, []string) {
	if stepCount(comps) < 2 {
		return nil, nil
	}
	var gaps []GapStep
	var texts []string
	for ci, c := range comps {
		for si, st := range c.Steps {
			texts = append(texts, st.Text)
			if !st.GraphCue && !st.GraphFromLLM && !st.GraphByUser {
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

// Stage kind vocabulary (ADR-00022): the verbs that say what a step is doing,
// for the time ribbon. Only the verbs that name one kind are here; "freeze",
// "ferment" and "soak" are passive but fit no kind, and "cool" is not "chill".
const (
	cookVerbs = `(saut(?:e|é)|fry|stir-fry|boil|sear|grill|broil|cook|brown|poach|steam|toast)`
	prepVerbs = `(chop|dice|slice|mince|peel|grate|trim)`
)

var (
	cookVerbRE = verbRE(cookVerbs)
	prepVerbRE = verbRE(prepVerbs)
	// stageWordRE picks the verb out of what verbRE matched, which also holds
	// the words around it.
	stageWordRE = regexp.MustCompile(`(?i)\b` + passiveVerbs + `\b`)
	stageByVerb = map[string]recipe.StageKind{
		"bake": recipe.StageBake, "roast": recipe.StageRoast, "simmer": recipe.StageSimmer,
		"braise": recipe.StageBraise, "marinate": recipe.StageMarinate, "steep": recipe.StageSteep,
		"chill": recipe.StageChill, "refrigerate": recipe.StageChill,
		"proof": recipe.StageRise, "prove": recipe.StageRise,
	}
	stageByWait = map[string]recipe.StageKind{
		"rest": recipe.StageRest, "stand": recipe.StageRest, "sit": recipe.StageRest,
		"rise": recipe.StageRise, "proof": recipe.StageRise, "prove": recipe.StageRise,
	}
	waitWordRE = regexp.MustCompile(`(?i)\b(rest|rise|stand|sit|cool|set|proof|prove)\b`)
)

// serveRE matches a step that is the serving: a title or text that opens with
// the word "serve" or "serving". Only the opening counts, so "Slice and serve"
// and "Stir before you serve" are left to the verb rules.
var serveRE = regexp.MustCompile(`(?i)^\W*serv(?:e|ing)\b`)

// Machine mixing and kneading (ADR-00022). Plain "mix", "stir", "whisk" and
// "combine" are not here: by hand they are prep or no kind, and they say
// nothing of a machine.
var (
	// mixWordRE matches a mixing machine named anywhere in the step ("in the
	// bowl of a stand mixer", "pulse in a food processor", "fit the dough
	// hook") and "knead" in any form but "kneaded", an adjective ("the kneaded
	// dough"), and "no-knead", a kind of bread.
	mixWordRE = regexp.MustCompile(`(?i)(^|[^\w-])knead(s|ing)?\b|\b(mixers?|food processors?|blenders?|bread machines?|dough hooks?)\b`)
	// mixVerbRE matches "blend" where it is the instruction, as verbRE reads
	// it, so "the spice blend" is not mixing.
	mixVerbRE = verbRE(`(blend)`)
)

// stageKindOf proposes a stage kind for a step from its source title and its
// text. A title that opens with "serve" says so outright, and so does a title
// whose only kind is mix ("Knead the dough"). In the text, an opening "serve"
// counts as the kind serve beside the kinds the verbs name, so serve alone is
// serve and serve with another kind is ambiguous and gets none (ADR-00022,
// ADR-00006).
func stageKindOf(title, text string) recipe.StageKind {
	if serveRE.MatchString(title) {
		return recipe.StageServe
	}
	if soleKind(stageKinds(title)) == recipe.StageMix {
		return recipe.StageMix
	}
	kinds := stageKinds(text)
	if serveRE.MatchString(text) {
		kinds[recipe.StageServe] = true
	}
	return soleKind(kinds)
}

// soleKind is the one kind in kinds, or "" when there are none or several.
func soleKind(kinds map[recipe.StageKind]bool) recipe.StageKind {
	if len(kinds) != 1 {
		return ""
	}
	for k := range kinds {
		return k
	}
	return ""
}

// stageKinds is every kind the text names. A step whose text names two
// different kinds ("let it rise, then bake") is ambiguous and gets none from
// soleKind: precision over recall, since an empty kind is only unknown and the
// user can set it on the review screen (ADR-00006).
func stageKinds(text string) map[recipe.StageKind]bool {
	kinds := map[recipe.StageKind]bool{}
	for _, m := range passiveVerbRE.FindAllString(text, -1) {
		if k, ok := stageByVerb[strings.ToLower(stageWordRE.FindString(m))]; ok {
			kinds[k] = true
		}
	}
	for _, m := range passiveStartRE.FindAllStringSubmatch(text, -1) {
		if k, ok := stageByWait[strings.ToLower(m[2])]; ok {
			kinds[k] = true
		}
	}
	for _, m := range passiveLetRE.FindAllString(text, -1) {
		if clauseSplitRE.MatchString(m) {
			continue
		}
		// The waiting word ends the phrase, so it is the last one in it.
		if w := waitWordRE.FindAllString(m, -1); len(w) > 0 {
			if k, ok := stageByWait[strings.ToLower(w[len(w)-1])]; ok {
				kinds[k] = true
			}
		}
	}
	if cookVerbRE.MatchString(text) {
		kinds[recipe.StageCook] = true
	}
	if prepVerbRE.MatchString(text) {
		kinds[recipe.StagePrep] = true
	}
	if mixWordRE.MatchString(text) || mixVerbRE.MatchString(text) {
		kinds[recipe.StageMix] = true
	}
	return kinds
}

// maxLeadWords is the longest lead sentence read as a step title.
const maxLeadWords = 5

// splitLead reads a step that runs its title into the instruction ("Preheat
// oven. Heat the oven to 375F.") as a title and the rest. The lead is the text
// up to the first full stop or colon that is followed by a space. It is a
// title only when it is at most maxLeadWords words with no digit, fraction or
// duration in it and no "!" or "?", and when more text follows; a step that is one
// sentence ("Cool on a rack before slicing.") has no title. ok is false for
// any other step, with title and rest empty. The rule is pure, so the same text
// always splits the same way (ADR-00006).
func splitLead(text string) (title, rest string, ok bool) {
	text = strings.TrimSpace(text)
	end := strings.IndexAny(text, ".:")
	if end < 0 || end+1 >= len(text) || text[end+1] != ' ' {
		return "", "", false
	}
	lead := strings.TrimSpace(text[:end])
	rest = strings.TrimSpace(text[end+1:])
	words := strings.Fields(lead)
	if len(words) == 0 || len(words) > maxLeadWords || rest == "" {
		return "", "", false
	}
	for _, r := range lead {
		if unicode.IsNumber(r) || r == '!' || r == '?' {
			return "", "", false
		}
	}
	// A worded duration ("an hour") is a quantity too.
	if len(parse.FindDurations(lead)) > 0 {
		return "", "", false
	}
	return strings.Join(words, " "), rest, true
}
