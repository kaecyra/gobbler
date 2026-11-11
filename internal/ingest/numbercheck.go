package ingest

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// numberCheckFactor scales the threshold to the confidence a line gets when
// tesseract did not read one of its numbers: below the threshold, so gap-fill
// treats the line as a gap and shows the photo to the vision model
// (ADR-00053, ADR-00020).
const numberCheckFactor = 0.5

// numberRE finds the numbers in a text, longest form first: a number with
// thousands separators, a vulgar fraction, a fraction written a/b (or with the
// fraction slash), a decimal, a whole number. A mixed number such as "1½" or
// "1 1/2" is its whole part and its fraction, found separately, so either
// spelling gives the same numbers.
var numberRE = regexp.MustCompile(`\d{1,3}(?:,\d{3})+(?:\.\d+)?|[½⅓⅔¼¾⅕⅖⅗⅘⅙⅚⅐⅛⅜⅝⅞⅑⅒]|\d+\s?[/⁄]\s?\d+|\d*\.\d+|\d+`)

// numbersIn is the set of numbers text states, by value, so ½, 1/2 and 0.5 are
// one number.
func numbersIn(text string) map[string]bool {
	set := map[string]bool{}
	for _, m := range numberRE.FindAllString(text, -1) {
		if k, ok := numberKey(m); ok {
			set[k] = true
		}
	}
	return set
}

// missingNumbers lists the numbers line states that are not in read, in the
// order line states them and once each.
func missingNumbers(line string, read map[string]bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range numberRE.FindAllString(line, -1) {
		k, ok := numberKey(m)
		if !ok || read[k] || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// numberKey is the canonical form of one number found by numberRE; false for
// one the quantity parser cannot read (such as a zero denominator), which is
// then no number to check.
func numberKey(m string) (string, bool) {
	m = strings.ReplaceAll(strings.ReplaceAll(m, ",", ""), " ", "")
	r, err := quantity.ParseRat(m)
	if err != nil {
		return "", false
	}
	return r.String(), true
}

// applyNumberCheck lowers the confidence of every ingredient line that states
// a number the tesseract text does not, below threshold, and says so in the
// line's issues. It is a pure function of the draft and the text, so replaying
// the same transcript and tesseract text gives the same flags (ADR-00053).
func applyNumberCheck(d *Draft, tesseract string, threshold float64) {
	read := numbersIn(tesseract)
	for ci := range d.Components {
		for li := range d.Components[ci].Lines {
			l := &d.Components[ci].Lines[li]
			missing := missingNumbers(l.Raw, read)
			if len(missing) == 0 {
				continue
			}
			l.Confidence = min(l.Confidence, threshold*numberCheckFactor)
			l.Issues = append(append([]string(nil), l.Issues...), fmt.Sprintf("number %s not found in the OCR text", strings.Join(missing, ", ")))
		}
	}
}
