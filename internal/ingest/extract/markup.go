package extract

import (
	stdhtml "html"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// blockElements are the elements that start a new line of text. One set is
// shared by the JSON-LD text cleaner (instruction strings carrying HTML) and
// the main-text fallback, so the two cannot drift apart.
var blockElements = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "main": true,
	"ul": true, "ol": true, "li": true, "br": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true, "td": true, "th": true,
	"blockquote": true, "dl": true, "dt": true, "dd": true, "figure": true,
	"figcaption": true, "pre": true, "fieldset": true,
}

var (
	// markupTagRE matches one tag, comment or declaration at the start of
	// the input. A "<" followed by anything else ("a < b") is plain text.
	markupTagRE = regexp.MustCompile(`^<(?:(/?)([a-zA-Z][a-zA-Z0-9]*)|[!?])[^>]*>`)
	// markupFractionRE matches a superscript numerator, a slash (also U+2044
	// and its entities) and a subscript or bare denominator.
	markupFractionRE = regexp.MustCompile(`(?is)^<sup[^>]*>\s*(\d+)\s*</sup>\s*(?:/|\x{2044}|&frasl;|&#8260;|&#x2044;)\s*(?:<sub[^>]*>\s*(\d+)\s*</sub>|(\d+))`)
	// markupSuffixRE matches a superscript that belongs to the word before
	// it: an ordinal suffix or a degree sign.
	markupSuffixRE = regexp.MustCompile(`(?is)^<sup[^>]*>\s*(st|nd|rd|th|\x{b0}|&deg;|&#176;)\s*</sup>`)
	// markupDegreeRE matches a superscript letter o or masculine ordinal
	// indicator, which after a digit is a degree sign ("350<sup>o</sup>F").
	markupDegreeRE = regexp.MustCompile(`(?is)^<sup[^>]*>\s*(?:o|\x{ba}|&ordm;|&#186;)\s*</sup>`)
	// markupFootnoteRE matches a superscript that is only digits.
	markupFootnoteRE = regexp.MustCompile(`(?is)^<sup[^>]*>\s*\d+\s*</sup>`)
	// markupEscapedBlockRE finds block-level tags that were entity-escaped.
	markupEscapedBlockRE = regexp.MustCompile(`(?i)&lt;/?(?:` + blockAlternation() + `)\b`)
)

func blockAlternation() string {
	names := make([]string, 0, len(blockElements))
	for n := range blockElements {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, "|")
}

// renderMarkup turns a string that may carry HTML into text, one line per
// block. Tags are not replaced by a character: a tag is a gap between two
// pieces of text the page wrote, and only at a gap are joining rules applied.
// Text without markup is entity-decoded and nothing else. Inline gaps:
//
//   - at whitespace, the start or the end: nothing (whitespace already there);
//   - before closing punctuation (. , ; : ! ? )) or after "(": nothing;
//   - between two letters or digits: one space, so "<b>a</b><i>b</i>" is
//     "a b" and "2 cups<i>flour</i>" does not fuse;
//   - anywhere else (a "/", "-", "%"): nothing, as the page rendered it.
//
// Block gaps are newlines. Special cases, all digits-and-markup shapes that
// otherwise read as a wrong quantity: a sup/sub fraction becomes "N a/b"
// (U+2044 and &frasl; normalised to "/"); an ordinal or degree sup
// ("1<sup>st</sup>", "350<sup>°</sup>F") joins with no gap; a sup that is only
// digits directly after a letter or closing punctuation ("flour,<sup>1</sup>")
// is a footnote marker and is dropped; a sup of "o" after a digit is a degree
// sign.
func renderMarkup(s string) string {
	var out strings.Builder
	gap := gapNone
	last := func() rune { r, _ := utf8.DecodeLastRuneInString(out.String()); return r }
	text := func(t string, joined bool) {
		t = stdhtml.UnescapeString(t)
		if t == "" {
			return
		}
		if !joined && out.Len() > 0 {
			switch gap {
			case gapBlock:
				out.WriteByte('\n')
			case gapInline:
				l, _ := utf8.DecodeRuneInString(t)
				if markupNeedsSpace(last(), l) {
					out.WriteByte(' ')
				}
			}
		}
		gap = gapNone
		out.WriteString(t)
	}
	setGap := func(g gapKind) {
		if g > gap {
			gap = g
		}
	}

	// A tag needs a closing ">", so a "<" after the last ">" is text. Checking
	// this once keeps the scan linear: each unclosed "<" would otherwise rescan
	// to the end of the input looking for one.
	lastGT := strings.LastIndexByte(s, '>')
	consumed := 0
	for s != "" {
		i := strings.IndexByte(s, '<')
		if i < 0 || consumed+i > lastGT {
			text(s, false)
			break
		}
		if i > 0 {
			text(s[:i], false)
			s = s[i:]
			consumed += i
		}
		before := len(s)
		if m := markupFractionRE.FindStringSubmatch(s); m != nil {
			setGap(gapInline)
			text(m[1]+"/"+m[2]+m[3], false)
			setGap(gapInline)
			s = s[len(m[0]):]
		} else if m := markupSuffixRE.FindStringSubmatch(s); m != nil {
			text(m[1], true)
			s = s[len(m[0]):]
		} else if m := markupDegreeRE.FindString(s); m != "" && unicode.IsDigit(last()) {
			text("\u00b0", true)
			s = s[len(m):]
		} else if m := markupFootnoteRE.FindString(s); m != "" && markupAfterWordOrClose(last()) {
			s = s[len(m):] // dropped: a footnote marker, not content
		} else if m := markupTagRE.FindStringSubmatch(s); m != nil {
			if blockElements[strings.ToLower(m[2])] {
				setGap(gapBlock)
			} else {
				setGap(gapInline)
			}
			s = s[len(m[0]):]
		} else {
			text("<", false)
			s = s[1:]
		}
		consumed += before - len(s)
	}
	return out.String()
}

type gapKind int

const (
	gapNone gapKind = iota
	gapInline
	gapBlock
)

// markupNeedsSpace decides an inline gap between the rune before it and the
// rune after it.
func markupNeedsSpace(before, after rune) bool {
	if unicode.IsSpace(before) || unicode.IsSpace(after) {
		return false
	}
	return isWordRune(before) && isWordRune(after)
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// markupAfterWordOrClose reports whether r ends a word or a clause, where a
// digits-only superscript is a footnote marker rather than content.
func markupAfterWordOrClose(r rune) bool {
	return unicode.IsLetter(r) || strings.ContainsRune(".,;:!?)", r)
}
