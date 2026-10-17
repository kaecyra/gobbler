package parse

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// Issues SplitSections reports for a labelled meta line it recognised but could
// not read. The line is then left in Notes, so nothing is dropped. The first
// two are also named by package extract (IssueUnparsedDuration) and the
// ingest assembler (IssueUnreadableYield); both alias these, so each code is
// spelled only here.
const (
	// IssueUnparsedDuration: a time field was not one clean duration.
	IssueUnparsedDuration = "unparsed_duration"
	// IssueUnreadableYield: a yield was stated but could not be read.
	IssueUnreadableYield = "unreadable_yield"
	// IssueUnreadableCalories: calories were stated but no number was found.
	IssueUnreadableCalories = "unreadable_calories"
)

// Meta is what labelled lines of pasted text state about the recipe as a
// whole. A zero field was not stated, or was stated and could not be read
// (which Sections.Issues says).
type Meta struct {
	Prep  time.Duration `json:"prep,omitempty"`
	Cook  time.Duration `json:"cook,omitempty"`
	Total time.Duration `json:"total,omitempty"`
	// Yield is the text after the label, starting with a quantity: "4" for
	// "Serves 4", "12 cookies" for "Makes 12 cookies".
	Yield    string `json:"yield,omitempty"`
	Calories int    `json:"calories,omitempty"`
}

type metaField int

const (
	metaPrep metaField = iota
	metaCook
	metaTotal
	metaYield
	metaCalories
)

var (
	// metaLabelRE splits "Prep time: 20 min" or "Serves 4". "active time" is
	// not matched: it has no field, so it stays in the notes.
	metaLabelRE = regexp.MustCompile(`(?i)^(prep(?:aration)?(?:\s+time)?|cook(?:ing)?(?:\s+time)?|total(?:\s+time)?|ready\s+in|servings?|serves|makes|yields?|calories(?:\s+per\s+serving)?|kcal)\s*(:)?\s*(\S.*)$`)
	// metaSepRE splits several fields written on one line.
	metaSepRE      = regexp.MustCompile(`\s*[|•·;]\s*`)
	metaCaloriesRE = regexp.MustCompile(`(?i)^(\d{1,3}(?:,\d{3})+|\d+)(?:\s*(?:k?cal(?:ories)?)\b)?\.?$`)
)

// metaFieldOf maps a matched label to its field.
func metaFieldOf(label string) metaField {
	label = strings.Join(strings.Fields(strings.ToLower(label)), " ")
	switch {
	case strings.HasPrefix(label, "prep"):
		return metaPrep
	case strings.HasPrefix(label, "cook"):
		return metaCook
	case strings.HasPrefix(label, "total"), label == "ready in":
		return metaTotal
	case strings.HasPrefix(label, "cal"), label == "kcal":
		return metaCalories
	}
	return metaYield
}

// metaLine reads one line of text as labelled meta fields into m, and reports
// whether it consumed the line. A line is consumed whole or not at all: every
// segment must carry a known label and a readable value for a field not yet
// set. A segment with a colon after its label is stated meta, so an unreadable
// value is reported in issues; without a colon ("Cook 20 minutes per side") it
// is only taken when the value reads cleanly, since prose starts that way too.
//
// inSteps narrows this for a line inside the steps section, where prose is
// common: only a colon-labelled line that reads cleanly is taken ("Calories:
// 280" is; "Serves 4" and "Cook 20 minutes" are not), and an unreadable one is
// never reported, it stays a step.
func metaLine(line string, m *Meta, inSteps bool) (bool, []string) {
	next := *m
	var issues []string
	for _, seg := range metaSepRE.Split(line, -1) {
		match := metaLabelRE.FindStringSubmatch(strings.TrimSpace(seg))
		if match == nil {
			return false, issues
		}
		f, stated, val := metaFieldOf(match[1]), match[2] != "", strings.TrimSpace(match[3])
		if inSteps && !stated {
			return false, nil
		}
		ok, issue := next.set(f, val)
		if !ok {
			if inSteps {
				return false, nil
			}
			if stated && issue != "" {
				issues = append(issues, issue)
			}
			return false, issues
		}
	}
	*m = next
	return true, nil
}

// set reads val into field f. It reports false, with the issue code for an
// unreadable value, when val cannot be read or the field is already set.
func (m *Meta) set(f metaField, val string) (bool, string) {
	switch f {
	case metaPrep, metaCook, metaTotal:
		dst := &m.Prep
		switch f {
		case metaCook:
			dst = &m.Cook
		case metaTotal:
			dst = &m.Total
		}
		d := Duration(val)
		if !d.Found || d.Range || d.Confidence < 1 || d.Min <= 0 {
			return false, IssueUnparsedDuration
		}
		if *dst != 0 {
			return false, ""
		}
		*dst = d.Min
	case metaYield:
		if _, _, ok := LeadingQuantity(val); !ok {
			return false, IssueUnreadableYield
		}
		if m.Yield != "" {
			return false, ""
		}
		m.Yield = val
	case metaCalories:
		c := metaCaloriesRE.FindStringSubmatch(val)
		if c == nil {
			return false, IssueUnreadableCalories
		}
		n, err := strconv.Atoi(strings.ReplaceAll(c[1], ",", ""))
		if err != nil || n <= 0 {
			return false, IssueUnreadableCalories
		}
		if m.Calories != 0 {
			return false, ""
		}
		m.Calories = n
	}
	return true, ""
}

// LeadingQuantity splits "1 1/2 cups" into a quantity and the rest.
func LeadingQuantity(s string) (quantity.Quantity, string, bool) {
	toks := strings.Fields(s)
	for n := min(3, len(toks)); n >= 1; n-- {
		if q, err := quantity.ParseQuantity(strings.Join(toks[:n], " ")); err == nil {
			return q, strings.Join(toks[n:], " "), true
		}
	}
	return quantity.Quantity{}, "", false
}
