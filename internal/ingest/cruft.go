package ingest

import (
	"regexp"
	"strings"
	"unicode"
)

// Blog cruft is text a recipe site writes for its own page, not for the
// recipe: a pointer to another part of the page, a video call-out, a sign-off.
// Kept in a saved recipe it points at nothing. cleanDescription removes it by
// rule (ADR-00006 rule-first), and the rules live here and nowhere else.
//
// Rules are deliberately narrow. A sentence goes only when a pattern below
// names it; a sentence that carries cooking information ("Cook Time assumes
// chicken is cooked in two batches.") matches none and stays.

// cruftCallOutRE is a parenthetical call-out to a video: "(Watch the video)".
var cruftCallOutRE = regexp.MustCompile(`(?i)\s*\((?:watch|see|click|view)\b[^()]*\bvideo\b[^()]*\)`)

// cruftSentences are tested against a whole sentence, lower-cased with curly
// apostrophes straightened. pointer marks a pointer to another page section:
// the sentence after it that starts with "it" or "this" describes what the
// pointer points at, so it goes too.
var cruftSentences = []struct {
	name    string
	re      *regexp.Regexp
	pointer bool
}{
	{"section pointer", regexp.MustCompile(
		`^(?:(?:please )?(?:don't forget to|be sure to|make sure to|remember to) )?` +
			`(?:see|check|read|visit|look at|have a look at|take a look at|refer to) ` +
			`(?:the|my|our) [^.!?]*\b(?:section|below|above|recipe card|recipe notes|post)\b`), true},
	{"scroll", regexp.MustCompile(`\bscroll (?:down|up|to)\b`), true},
	{"jump to recipe", regexp.MustCompile(`\bjump to (?:the )?recipe\b`), false},
	{"video invitation", regexp.MustCompile(`^(?:watch|see) (?:the|my|our) (?:how-?to )?video\b`), false},
	{"pin", regexp.MustCompile(`\bpin (?:this|it)\b|\bsave (?:this|it) (?:to|on) pinterest\b`), false},
	{"subscribe", regexp.MustCompile(`\bsubscribe\b|\b(?:sign up|signup) (?:for|to) (?:my|our) newsletter\b|\bjoin (?:my|our) newsletter\b`), false},
	{"sign-off", regexp.MustCompile(`^i hope (?:it|this|you)\b[^.!?]*\b(?:helps?|helpful|of help|enjoy)\b`), false},
}

// cruftAnaphoraRE starts a sentence that refers back to the pointer before it.
var cruftAnaphoraRE = regexp.MustCompile(`^(?:it|this) `)

// sentenceAbbreviations are words whose trailing period does not end a
// sentence and which a missing space after must not be "repaired" around.
var sentenceAbbreviations = map[string]bool{
	"approx": true, "tbsp": true, "tsp": true, "oz": true, "lb": true, "lbs": true,
	"vs": true, "etc": true, "min": true, "mins": true, "sec": true, "temp": true,
	"dr": true, "mr": true, "mrs": true, "ms": true, "st": true, "no": true,
	"eg": true, "ie": true, "pkg": true, "ca": true, "max": true,
}

// cleanDescription returns a description without blog cruft and with sentence
// spacing restored where paragraphs were fused ("batches.Don't"). It is pure.
func cleanDescription(s string) string {
	s = cruftCallOutRE.ReplaceAllString(s, " ")
	s = strings.NewReplacer("’", "'", "‘", "'").Replace(s)
	s = restoreSentenceSpacing(s)
	var kept []string
	dropNextAnaphor := false
	for _, sent := range splitSentences(s) {
		low := strings.ToLower(sent)
		switch drop, pointer := matchCruft(low); {
		case drop:
			dropNextAnaphor = pointer
			continue
		case dropNextAnaphor && cruftAnaphoraRE.MatchString(low):
			dropNextAnaphor = false
			continue
		}
		dropNextAnaphor = false
		kept = append(kept, sent)
	}
	return strings.Join(kept, " ")
}

func matchCruft(lowerSentence string) (drop, pointer bool) {
	for _, r := range cruftSentences {
		if r.re.MatchString(lowerSentence) {
			return true, r.pointer
		}
	}
	return false, false
}

// restoreSentenceSpacing inserts the missing space in "word.Word", "word!Word"
// and "word?Word": a sentence mark right after a lower-case letter and right
// before a capital. The word before the mark must be at least two letters and
// not an abbreviation, so "e.g.Foo" style text, initials and decimals are left
// as written.
func restoreSentenceSpacing(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		b.WriteRune(r)
		if (r == '.' || r == '!' || r == '?') && i+1 < len(runes) && unicode.IsUpper(runes[i+1]) &&
			i > 0 && unicode.IsLower(runes[i-1]) {
			w := wordBefore(runes, i)
			if len([]rune(w)) >= 2 && !sentenceAbbreviations[strings.ToLower(w)] {
				b.WriteByte(' ')
			}
		}
	}
	return b.String()
}

// wordBefore is the run of letters, dots and digits ending just before
// runes[i], without a trailing dot: "e.g" for "e.g.", "batches" for "batches.".
// A run holding a dot or digit is not a plain word and returns "" so no repair
// is made inside it.
func wordBefore(runes []rune, i int) string {
	j := i
	for j > 0 && (unicode.IsLetter(runes[j-1]) || unicode.IsDigit(runes[j-1]) || runes[j-1] == '.') {
		j--
	}
	w := string(runes[j:i])
	if strings.ContainsAny(w, ".0123456789") {
		return ""
	}
	return w
}

// splitSentences splits at a sentence mark followed by whitespace, except
// after an abbreviation or a single letter. Whitespace runs collapse.
func splitSentences(s string) []string {
	fields := strings.Fields(s)
	var out []string
	var cur []string
	for _, f := range fields {
		cur = append(cur, f)
		if endsSentence(f) {
			out = append(out, strings.Join(cur, " "))
			cur = nil
		}
	}
	if len(cur) > 0 {
		out = append(out, strings.Join(cur, " "))
	}
	return out
}

func endsSentence(word string) bool {
	trimmed := strings.TrimRight(word, `)"'`)
	if trimmed == "" {
		return false
	}
	last := trimmed[len(trimmed)-1]
	if last != '.' && last != '!' && last != '?' {
		return false
	}
	if last != '.' {
		return true
	}
	bare := strings.ToLower(strings.TrimRight(trimmed, "."))
	bare = strings.TrimLeft(bare, `("'`)
	r := []rune(bare)
	if len(r) == 1 {
		return unicode.IsDigit(r[0])
	}
	return !sentenceAbbreviations[bare] && !strings.Contains(bare, ".")
}
