package parse

import "strings"

// LineQualifierKind says what a qualifier on an ingredient line describes.
type LineQualifierKind string

// Qualifier kinds (ADR-00008: qualifiers are not part of the ingredient name).
const (
	// LineQualPreparation is work done to the ingredient: chopped, minced.
	LineQualPreparation LineQualifierKind = "preparation"
	// LineQualState is the condition it is in: melted, softened, frozen.
	LineQualState LineQualifierKind = "state"
	// LineQualForm is the variety or form bought: fresh, dried, ground.
	LineQualForm LineQualifierKind = "form"
	// LineQualNote is anything else worth keeping: large, divided, for serving.
	LineQualNote LineQualifierKind = "note"
)

// lineQualifierWords classifies single words. Reviewed as data: add a word
// here and add a corpus line that uses it.
var lineQualifierWords = map[string]LineQualifierKind{
	// preparation
	"chopped": LineQualPreparation, "diced": LineQualPreparation, "minced": LineQualPreparation,
	"sliced": LineQualPreparation, "grated": LineQualPreparation, "shredded": LineQualPreparation,
	"crushed": LineQualPreparation, "cubed": LineQualPreparation, "julienned": LineQualPreparation,
	"peeled": LineQualPreparation, "trimmed": LineQualPreparation, "halved": LineQualPreparation,
	"quartered": LineQualPreparation, "mashed": LineQualPreparation, "beaten": LineQualPreparation,
	"whisked": LineQualPreparation, "sifted": LineQualPreparation, "drained": LineQualPreparation,
	"rinsed": LineQualPreparation, "torn": LineQualPreparation, "crumbled": LineQualPreparation,
	"cored": LineQualPreparation, "seeded": LineQualPreparation, "pitted": LineQualPreparation,
	"deveined": LineQualPreparation, "zested": LineQualPreparation, "juiced": LineQualPreparation,
	"toasted": LineQualPreparation, "pureed": LineQualPreparation, "ground": LineQualPreparation,
	"cut": LineQualPreparation, "separated": LineQualPreparation, "stemmed": LineQualPreparation,

	// state
	"melted": LineQualState, "softened": LineQualState, "cold": LineQualState,
	"chilled": LineQualState, "frozen": LineQualState, "thawed": LineQualState,
	"warm": LineQualState, "hot": LineQualState, "cooled": LineQualState,
	"boiling": LineQualState, "lukewarm": LineQualState,

	// form
	"fresh": LineQualForm, "dried": LineQualForm, "dry": LineQualForm,
	"whole": LineQualForm, "canned": LineQualForm, "raw": LineQualForm,
	"cooked": LineQualForm, "uncooked": LineQualForm, "smoked": LineQualForm,
	"boneless": LineQualForm, "skinless": LineQualForm, "ripe": LineQualForm,
	"unsalted": LineQualForm, "salted": LineQualForm, "sweetened": LineQualForm,
	"unsweetened": LineQualForm,

	// note
	"large": LineQualNote, "medium": LineQualNote, "small": LineQualNote,
	"packed": LineQualNote, "divided": LineQualNote, "heaping": LineQualNote,
	"level": LineQualNote, "scant": LineQualNote,
}

// lineQualifierPhrases are multi-word qualifiers, longest first.
var lineQualifierPhrases = []struct {
	text string
	kind LineQualifierKind
}{
	{"at room temperature", LineQualState},
	{"room temperature", LineQualState},
	{"extra large", LineQualNote},
	{"extra-large", LineQualNote},
}

// lineAdverbs modify a qualifier word: "finely chopped", "freshly ground".
var lineAdverbs = map[string]bool{
	"finely": true, "coarsely": true, "roughly": true, "thinly": true,
	"thickly": true, "freshly": true, "lightly": true, "well": true,
	"very": true, "firmly": true, "loosely": true, "evenly": true,
}

// lineNotePrefixes begin a tail segment that is a known kind of note.
var lineNotePrefixes = []string{
	"for ", "plus ", "divided", "or more", "or less", "as needed", "to serve",
	"cut into", "cut in", "about ", "approximately ", "see ", "such as ", "like ",
	"preferably", "if ", "any ", "your ", "more ", "at ",
}

// lineLeadingQualifier reads one qualifier from the front of words (an
// optional adverb then a qualifier word, or a known phrase). It returns the
// kind, the qualifier text and how many words it used; n is zero when words
// do not start with a qualifier.
func lineLeadingQualifier(words []string) (kind LineQualifierKind, text string, n int) {
	for _, p := range lineQualifierPhrases {
		pw := strings.Fields(p.text)
		if len(words) >= len(pw) && strings.EqualFold(strings.Join(words[:len(pw)], " "), p.text) {
			return p.kind, p.text, len(pw)
		}
	}
	w0 := strings.ToLower(words[0])
	if lineAdverbs[w0] && len(words) > 1 {
		if k, ok := lineQualifierWords[strings.ToLower(words[1])]; ok {
			return k, w0 + " " + strings.ToLower(words[1]), 2
		}
		return "", "", 0
	}
	if k, ok := lineQualifierWords[w0]; ok {
		return k, w0, 1
	}
	return "", "", 0
}

// lineClassifySegment classifies one comma-separated tail segment. recognized
// is false when nothing in the vocabulary explains it; it is then kept as a
// note and counted as leftover text by the caller.
func lineClassifySegment(seg string) (kind LineQualifierKind, recognized bool) {
	words := strings.Fields(strings.ToLower(seg))
	if len(words) == 0 {
		return LineQualNote, true
	}
	if k, _, n := lineLeadingQualifier(words); n > 0 {
		return k, true
	}
	low := strings.ToLower(seg)
	for _, p := range lineNotePrefixes {
		if strings.HasPrefix(low, p) {
			return LineQualNote, true
		}
	}
	return LineQualNote, false
}

// lineSplitSegments splits a tail on commas and semicolons outside brackets,
// then splits each piece on " and " when every part is a recognised
// qualifier ("peeled and diced").
func lineSplitSegments(tail string) []string {
	var out []string
	for _, piece := range lineSplitTop(tail, ",;") {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}
		parts := strings.Split(piece, " and ")
		if len(parts) > 1 {
			all := true
			for _, p := range parts {
				if _, ok := lineClassifySegment(strings.TrimSpace(p)); !ok {
					all = false
					break
				}
			}
			if all {
				for _, p := range parts {
					out = append(out, strings.TrimSpace(p))
				}
				continue
			}
		}
		out = append(out, piece)
	}
	return out
}

// lineSplitTop splits s at any rune of seps that is outside parentheses and
// brackets and is not a thousands comma between digits.
func lineSplitTop(s, seps string) []string {
	var out []string
	depth, start := 0, 0
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case r == '(' || r == '[':
			depth++
		case r == ')' || r == ']':
			if depth > 0 {
				depth--
			}
		case depth == 0 && strings.ContainsRune(seps, r):
			if r == ',' && i > 0 && i+1 < len(rs) && lineIsDigit(rs[i-1]) && lineIsDigit(rs[i+1]) {
				continue
			}
			out = append(out, string(rs[start:i]))
			start = i + 1
		}
	}
	return append(out, string(rs[start:]))
}

func lineIsDigit(r rune) bool { return r >= '0' && r <= '9' }
