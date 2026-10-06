package parse

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrEmptyText is returned by SplitSections when the text has no content. It
// is the only error: unclear structure is reported through Sections.Issues.
var ErrEmptyText = errors.New("text is empty")

// SectionIssue names one reason a sectioning result is less than certain. The
// review screen (ADR-00006) shows these; they are data, not failures.
type SectionIssue string

// Issues SplitSections can report.
const (
	// IssueNoTitle: no title line was found.
	IssueNoTitle SectionIssue = "no_title"
	// IssueNoIngredients: no ingredient lines were found.
	IssueNoIngredients SectionIssue = "no_ingredients"
	// IssueNoSteps: no step texts were found.
	IssueNoSteps SectionIssue = "no_steps"
	// IssueNoHeadings: the text has no Ingredients or Directions heading, so
	// the split is a guess from the shape of each line.
	IssueNoHeadings SectionIssue = "no_headings"
	// IssueImplicitBoundary: an ingredient or step section started without a
	// heading, so one boundary is a guess.
	IssueImplicitBoundary SectionIssue = "implicit_boundary"
)

// Confidence penalties applied to a perfect score of 1 by SplitSections.
const (
	sectionPenaltyNoIngredients = 0.4
	sectionPenaltyNoSteps       = 0.4
	sectionPenaltyNoTitle       = 0.1
	sectionPenaltyNoHeadings    = 0.3
	sectionPenaltyImplicit      = 0.1
)

// sectionMaxTitleRunes bounds a line that can be taken as the title.
const sectionMaxTitleRunes = 100

// SectionComponent is one named part of a recipe ("sauce", "dough") with its
// raw, unparsed lines. Name is empty for the unnamed component.
type SectionComponent struct {
	Name        string   `json:"name"`
	Ingredients []string `json:"ingredients"`
	Steps       []string `json:"steps"`
}

// Sections is the result of splitting pasted recipe text. Components are in
// source order. Notes holds text that belongs to neither ingredients nor
// steps (a description, a "Notes" section) so nothing is silently dropped.
type Sections struct {
	Title      string             `json:"title"`
	Components []SectionComponent `json:"components"`
	Notes      []string           `json:"notes"`
	// Confidence is 0 to 1; Issues says why it is below 1.
	Confidence float64        `json:"confidence"`
	Issues     []SectionIssue `json:"issues"`
}

type sectionMode int

const (
	sectionPreamble sectionMode = iota
	sectionIngredients
	sectionSteps
	sectionNotes
)

var (
	sectionIngredientHeadingRe = regexp.MustCompile(`^(?:ingredients?(?: list)?|what you(?:'ll| will)? need)$`)
	sectionStepsHeadingRe      = regexp.MustCompile(`^(?:directions?|methods?|instructions?|preparation|procedure|steps|how to make(?: it)?)$`)
	sectionNotesHeadingRe      = regexp.MustCompile(`^(?:notes?|tips?|nutrition(?: facts| information)?|storage|chef'?s notes?)$`)
	sectionForHeadingRe        = regexp.MustCompile(`^for (?:the )?([^.,;:!?]+?):?$`)
	sectionBulletRe            = regexp.MustCompile(`^(?:[-*+•▪·–—☐□]|\[[ xX]?\])\s+`)
	sectionStepMarkerRe        = regexp.MustCompile(`(?i)^(?:step\s+)?\d+\s*(?:[.):]\s*|$)`)
	sectionQuantityStartRe     = regexp.MustCompile(`^(?:\d+(?:[./,]\d+)?|\d+-\d+/\d+|[½⅓⅔¼¾⅕⅖⅗⅘⅙⅚⅛⅜⅝⅞]|\d+[½⅓⅔¼¾⅛⅜⅝⅞])(?:\s|$)`)
)

// rawStepLine is a step-section line kept until its component is finalised.
type rawStepLine struct {
	text        string
	blankBefore bool
}

type sectionBuilder struct {
	comps      []SectionComponent
	stepLines  [][]rawStepLine
	cur        int
	implicit   bool
	hasHeading bool
}

func (b *sectionBuilder) open(name string) {
	for i, c := range b.comps {
		if name != "" && strings.EqualFold(c.Name, name) {
			b.cur = i
			return
		}
	}
	// A still-empty unnamed component is renamed rather than left behind.
	if n := len(b.comps); n > 0 && b.cur == n-1 && b.comps[n-1].Name == "" &&
		len(b.comps[n-1].Ingredients) == 0 && len(b.stepLines[n-1]) == 0 && name != "" {
		b.comps[n-1].Name = name
		return
	}
	b.comps = append(b.comps, SectionComponent{Name: name})
	b.stepLines = append(b.stepLines, nil)
	b.cur = len(b.comps) - 1
}

func (b *sectionBuilder) hasComponent(name string) bool {
	for _, c := range b.comps {
		if strings.EqualFold(c.Name, name) {
			return true
		}
	}
	return false
}

func (b *sectionBuilder) addIngredient(s string) {
	if len(b.comps) == 0 {
		b.open("")
	}
	b.comps[b.cur].Ingredients = append(b.comps[b.cur].Ingredients, s)
}

func (b *sectionBuilder) addStepLine(s string, blankBefore bool) {
	if len(b.comps) == 0 {
		b.open("")
	}
	b.stepLines[b.cur] = append(b.stepLines[b.cur], rawStepLine{s, blankBefore})
}

// SplitSections splits pasted recipe text into a title and ordered components,
// each with raw ingredient lines and raw step texts. It does not parse the
// lines. It is pure and deterministic. Whitespace-only text returns
// ErrEmptyText; any other text returns a result, with Confidence and Issues
// reporting what could not be found.
func SplitSections(text string) (Sections, error) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if strings.TrimSpace(text) == "" {
		return Sections{}, ErrEmptyText
	}
	lines := strings.Split(text, "\n")
	explicit := false
	for _, l := range lines {
		if k := sectionKeyword(l); k == sectionIngredients || k == sectionSteps {
			explicit = true
			break
		}
	}

	res := Sections{Components: []SectionComponent{}, Notes: []string{}, Issues: []SectionIssue{}}
	var b sectionBuilder
	mode := sectionPreamble
	blank := true
	seenIngredient := false
	firstLine := true

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			blank = true
			continue
		}
		wasBlank, wasFirst := blank, firstLine
		blank, firstLine = false, false

		if k := sectionKeyword(line); k != sectionPreamble {
			mode = k
			b.hasHeading = true
			if k == sectionIngredients || k == sectionSteps {
				// A section heading ends component scope only for the first
				// section; later steps reuse components by name.
				if k == sectionIngredients && len(b.comps) > 0 {
					b.open("")
				}
			}
			if k == sectionSteps && len(b.comps) > 0 {
				b.cur = 0
			}
			continue
		}

		stripped := sectionStripDecor(line)
		if mode == sectionPreamble && res.Title == "" && !seenIngredient && !sectionForHeadingRe.MatchString(strings.ToLower(stripped)) &&
			!strings.HasSuffix(stripped, ":") && sectionTitleLike(stripped, explicit, wasFirst) {
			res.Title = stripped
			continue
		}
		if name, ok := sectionComponentHeading(stripped, line, mode, wasBlank, sectionNextNonBlank(lines, i), &b); ok {
			if mode == sectionPreamble || mode == sectionNotes {
				mode = sectionIngredients
				b.implicit = true
			}
			b.open(name)
			continue
		}

		switch mode {
		case sectionPreamble:
			switch {
			case !explicit && sectionIngredientLike(line):
				mode = sectionIngredients
				b.implicit = true
				seenIngredient = true
				b.addIngredient(sectionStripBullet(line))
			case !explicit && seenIngredient:
				mode = sectionSteps
				b.implicit = true
				b.addStepLine(line, wasBlank)
			case !explicit && sectionSentence(stripped):
				mode = sectionSteps
				b.implicit = true
				b.addStepLine(line, wasBlank)
			default:
				res.Notes = append(res.Notes, stripped)
			}
		case sectionIngredients:
			seenIngredient = true
			if !sectionIngredientLike(line) && ((!explicit && sectionSentence(stripped)) || strings.HasSuffix(stripped, ".")) {
				mode = sectionSteps
				b.implicit = true
				b.addStepLine(line, wasBlank)
				continue
			}
			b.addIngredient(sectionStripBullet(line))
		case sectionSteps:
			b.addStepLine(line, wasBlank)
		case sectionNotes:
			res.Notes = append(res.Notes, stripped)
		}
	}

	for i := range b.comps {
		b.comps[i].Steps = sectionJoinSteps(b.stepLines[i])
		if b.comps[i].Ingredients == nil {
			b.comps[i].Ingredients = []string{}
		}
		if len(b.comps[i].Ingredients) > 0 || len(b.comps[i].Steps) > 0 {
			res.Components = append(res.Components, b.comps[i])
		}
	}
	sectionScore(&res, b.hasHeading, b.implicit)
	return res, nil
}

func sectionScore(res *Sections, hasHeading, implicit bool) {
	var ing, steps int
	for _, c := range res.Components {
		ing += len(c.Ingredients)
		steps += len(c.Steps)
	}
	score := 1.0
	add := func(is SectionIssue, penalty float64) {
		res.Issues = append(res.Issues, is)
		score -= penalty
	}
	if res.Title == "" {
		add(IssueNoTitle, sectionPenaltyNoTitle)
	}
	if ing == 0 {
		add(IssueNoIngredients, sectionPenaltyNoIngredients)
	}
	if steps == 0 {
		add(IssueNoSteps, sectionPenaltyNoSteps)
	}
	switch {
	case !hasHeading:
		add(IssueNoHeadings, sectionPenaltyNoHeadings)
	case implicit:
		add(IssueImplicitBoundary, sectionPenaltyImplicit)
	}
	if score < 0 {
		score = 0
	}
	// Round away float noise so output is stable in golden files.
	res.Confidence = float64(int(score*100+0.5)) / 100
}

// sectionStripDecor removes markdown heading and emphasis markers.
func sectionStripDecor(s string) string {
	s = strings.TrimSpace(strings.TrimLeft(s, "#"))
	for _, d := range []string{"**", "__"} {
		if len(s) > 2*len(d) && strings.HasPrefix(s, d) && strings.Contains(s[len(d):], d) {
			s = strings.Replace(s[len(d):], d, "", 1)
		}
	}
	return strings.TrimSpace(s)
}

func sectionStripBullet(s string) string {
	return strings.TrimSpace(sectionBulletRe.ReplaceAllString(s, ""))
}

// sectionKeyword reports whether a line is an ingredients, steps or notes
// heading, or sectionPreamble for none of them.
func sectionKeyword(line string) sectionMode {
	s := strings.ToLower(strings.TrimRight(sectionStripDecor(strings.TrimSpace(line)), ": "))
	s = strings.TrimSpace(strings.TrimSuffix(s, ":"))
	switch {
	case sectionIngredientHeadingRe.MatchString(s):
		return sectionIngredients
	case sectionStepsHeadingRe.MatchString(s):
		return sectionSteps
	case sectionNotesHeadingRe.MatchString(s):
		return sectionNotes
	}
	return sectionPreamble
}

// sectionComponentHeading decides whether a line names a component. "For the
// X" and a short colon-ended line count anywhere in ingredients; elsewhere
// only "For the X" and names already seen do, so a step such as "Mix the
// following:" is not taken for a heading. An unmarked short line counts only
// when set apart by a blank line before it, in ingredient context.
func sectionComponentHeading(stripped, line string, mode sectionMode, blankBefore bool, next string, b *sectionBuilder) (string, bool) {
	if sectionBulletRe.MatchString(line) || sectionStepMarkerRe.MatchString(stripped) || sectionQuantityStartRe.MatchString(stripped) {
		return "", false
	}
	low := strings.ToLower(stripped)
	words := len(strings.Fields(stripped))
	if m := sectionForHeadingRe.FindStringSubmatch(low); m != nil && words <= 7 {
		// Keep the original casing of the captured name.
		name := strings.TrimSpace(strings.TrimSuffix(stripped, ":"))
		name = strings.TrimSpace(name[len(name)-len(m[1]):])
		return name, true
	}
	name := strings.TrimSpace(strings.TrimSuffix(stripped, ":"))
	if name == "" || strings.ContainsAny(name, ".!?;,") || words > 4 {
		return "", false
	}
	if b.hasComponent(name) && (mode == sectionSteps || mode == sectionIngredients) {
		return name, true
	}
	if mode == sectionSteps || mode == sectionNotes {
		return "", false
	}
	colon := strings.HasSuffix(stripped, ":")
	if colon || (blankBefore && next != "" && mode == sectionIngredients && sectionHeadingShaped(name)) {
		return name, true
	}
	return "", false
}

// sectionHeadingShaped reports a short capitalised or all-caps phrase.
func sectionHeadingShaped(s string) bool {
	for _, w := range strings.Fields(s) {
		r, _ := utf8.DecodeRuneInString(w)
		if !unicode.IsUpper(r) {
			return false
		}
	}
	return !strings.ContainsFunc(s, unicode.IsDigit)
}

func sectionNextNonBlank(lines []string, i int) string {
	for _, l := range lines[i+1:] {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

func sectionIngredientLike(line string) bool {
	if sectionBulletRe.MatchString(line) {
		return true
	}
	return sectionQuantityStartRe.MatchString(line) && !sectionStepMarkerRe.MatchString(line)
}

func sectionSentence(s string) bool {
	return strings.HasSuffix(s, ".") || strings.HasSuffix(s, "!") || strings.HasSuffix(s, "?") || len(strings.Fields(s)) >= 8
}

func sectionTitleLike(s string, explicit, first bool) bool {
	if utf8.RuneCountInString(s) > sectionMaxTitleRunes || strings.HasSuffix(s, ".") || sectionBulletRe.MatchString(s) {
		return false
	}
	if explicit {
		// A numeric first line ("7-Layer Dip") is still the title when real
		// headings follow.
		return first || !sectionQuantityStartRe.MatchString(s)
	}
	return !sectionQuantityStartRe.MatchString(s)
}

// sectionJoinSteps turns raw step lines into step texts with markers removed.
// With numbered or bulleted markers, an unmarked line continues the previous
// step. Without markers, blank-separated paragraphs are steps, or each line is
// one step when there are no paragraph breaks.
func sectionJoinSteps(lines []rawStepLine) []string {
	steps := []string{}
	if len(lines) == 0 {
		return steps
	}
	marked, paragraphs := false, false
	for i, l := range lines {
		if sectionStepMarkerRe.MatchString(l.text) || sectionBulletRe.MatchString(l.text) {
			marked = true
		}
		if i > 0 && l.blankBefore {
			paragraphs = true
		}
	}
	for i, l := range lines {
		text := l.text
		isMarker := false
		if loc := sectionStepMarkerRe.FindStringIndex(text); loc != nil {
			text, isMarker = strings.TrimSpace(text[loc[1]:]), true
		} else if loc := sectionBulletRe.FindStringIndex(text); loc != nil {
			text, isMarker = strings.TrimSpace(text[loc[1]:]), true
		}
		var startNew bool
		switch {
		case marked:
			startNew = isMarker || len(steps) == 0
		case paragraphs:
			startNew = l.blankBefore || i == 0
		default:
			startNew = true
		}
		switch {
		case startNew && text == "":
			steps = append(steps, "")
		case startNew:
			steps = append(steps, text)
		case len(steps) > 0 && steps[len(steps)-1] == "" && marked:
			steps[len(steps)-1] = text
		default:
			steps[len(steps)-1] += " " + text
		}
	}
	out := steps[:0]
	for _, s := range steps {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
