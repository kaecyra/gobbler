package parse

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// DurationResult is the reading of a time field or a span of step text.
type DurationResult struct {
	// Found is false when no duration could be read; every other field is then
	// zero and the caller treats the duration as absent.
	Found bool
	// Min is the duration, or the lower bound of a range.
	Min time.Duration
	// Max equals Min unless Range is set ("1-2 hours").
	Max   time.Duration
	Range bool
	// Text is the matched text. FindDurations also sets Start and End, the
	// byte offsets of Text in its input (End exclusive); Duration
	// normalises the field first, so it leaves them zero.
	Text       string
	Start, End int
	// Confidence is 1 for a clean read, lower when the field held text the
	// parser could not account for, and 0 when nothing was found.
	Confidence float64
}

// Duration confidence for a field that holds a duration plus other text.
const durationPartialConfidence = 0.5

const (
	durNum = `(?:\d+\s+\d+/\d+|\d+/\d+|\d*\.\d+|\d+\s*[` + lineFractionRunes + `]|[` + lineFractionRunes + `]|\d+)`
	// Words and common abbreviations; usable anywhere.
	durUnitWord = `(?:days?|hours?|hrs?|minutes?|mins?|seconds?|secs?)`
	// Single letters are only trusted in a field that is all duration.
	durUnitField = `(?:days?|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s|d)`
)

func durTermRE(unit string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)(?:\b(?P<art>half an|an?)\s+(?P<aunit>hour|minute)\b|(?P<lo>` + durNum +
		`)(?:\s*(?:-|–|—|to)\s*(?P<hi>` + durNum + `))?\s*(?P<unit>` + unit + `)\b)`)
}

var (
	durWordTermRE  = durTermRE(durUnitWord)
	durFieldTermRE = durTermRE(durUnitField)
	durISORE       = regexp.MustCompile(`(?i)^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)
	durJoinRE      = regexp.MustCompile(`(?i)^(?:\s*(?:and|,|&)?\s*)`)
	durDigitLetRE  = regexp.MustCompile(`(\d)([A-Za-z])`)
	durLetDigitRE  = regexp.MustCompile(`([A-Za-z])(\d)`)
	durHedgeRE     = regexp.MustCompile(`(?i)^\s*(?:about|approx\.?|approximately|around|~|up to|at least|for|in)?\s*`)
)

// Duration reads a time field such as "1 hr 30 min", "90 minutes",
// "1-2 hours", "1h30m", "half an hour" or ISO 8601 "PT1H30M". When the field
// holds more than a duration, the duration is still returned with a lower
// Confidence; when it holds none, Found is false.
func Duration(field string) DurationResult {
	s := strings.TrimSpace(field)
	if d, ok := durParseISO(s); ok {
		return DurationResult{Found: true, Min: d, Max: d, Text: s, Confidence: 1}
	}
	// "1h30m": the unit regexps need a word boundary, so separate digits from letters.
	s = durDigitLetRE.ReplaceAllString(s, "$1 $2")
	s = durLetDigitRE.ReplaceAllString(s, "$1 $2")
	r := durScan(s, durFieldTermRE, 0)
	if !r.Found {
		return DurationResult{}
	}
	rest := strings.TrimSpace(s[:r.Start] + s[r.End:])
	if rest != "" && strings.TrimSpace(durHedgeRE.ReplaceAllString(rest, "")) != "" {
		r.Confidence = durationPartialConfidence
	}
	r.Start, r.End = 0, 0
	return r
}

// FindDurations returns every duration written in step text, in order:
// "Bake 25 to 30 minutes, then rest 5 min" yields two. Single-letter units
// ("30m") are not trusted in prose; use Duration for a time field.
func FindDurations(text string) []DurationResult {
	var out []DurationResult
	for pos := 0; pos < len(text); {
		r := durScan(text, durWordTermRE, pos)
		if !r.Found {
			break
		}
		out = append(out, r)
		pos = r.End
	}
	return out
}

// durScan finds the first duration at or after from and extends it over
// adjacent terms: "1 hour 30 minutes", "1 hr and 15 min".
func durScan(text string, re *regexp.Regexp, from int) DurationResult {
	loc := re.FindStringSubmatchIndex(text[from:])
	if loc == nil {
		return DurationResult{}
	}
	start := from + loc[0]
	lo, hi, rng, end, unit, ok := durTerm(re, text, start)
	if !ok {
		// A number we cannot read ("1/0 hours"): skip past it.
		return durScan(text, re, start+max(loc[1]-loc[0], 1))
	}
	for {
		j := durJoinRE.FindStringIndex(text[end:])
		next := end + j[1]
		if next >= len(text) {
			break
		}
		l2, h2, r2, e2, u2, ok2 := durTerm(re, text, next)
		// Only "1 hour 30 minutes" joins; "10 minutes, 5 minutes more" is two.
		if !ok2 || r2 || u2 >= unit {
			break
		}
		lo, hi, end, unit = lo+l2, hi+h2, e2, u2
	}
	return DurationResult{
		Found: true, Min: lo, Max: hi, Range: rng,
		Text: text[start:end], Start: start, End: end, Confidence: 1,
	}
}

// durTerm reads the single term starting at start.
func durTerm(re *regexp.Regexp, text string, start int) (lo, hi time.Duration, rng bool, end int, unit time.Duration, ok bool) {
	m := re.FindStringSubmatchIndex(text[start:])
	if m == nil || m[0] != 0 {
		return 0, 0, false, 0, 0, false
	}
	get := func(name string) string {
		i := re.SubexpIndex(name)
		if i < 0 || m[2*i] < 0 {
			return ""
		}
		return text[start+m[2*i] : start+m[2*i+1]]
	}
	end = start + m[1]
	if art := strings.ToLower(get("art")); art != "" {
		unit = durUnit(get("aunit"))
		d := unit
		if art == "half an" {
			d = unit / 2
		}
		return d, d, false, end, unit, true
	}
	unit = durUnit(get("unit"))
	l, err := durNumber(get("lo"))
	if err != nil {
		return 0, 0, false, 0, 0, false
	}
	lo, hi = durScale(l, unit), durScale(l, unit)
	if h := get("hi"); h != "" {
		hv, err := durNumber(h)
		if err != nil || hv.Cmp(l) < 0 {
			return 0, 0, false, 0, 0, false
		}
		hi, rng = durScale(hv, unit), true
	}
	return lo, hi, rng, end, unit, true
}

func durNumber(s string) (quantity.Rat, error) {
	return quantity.ParseRat(strings.TrimSpace(s))
}

func durScale(r quantity.Rat, unit time.Duration) time.Duration {
	return time.Duration(math.Round(r.Float64() * float64(unit)))
}

func durUnit(s string) time.Duration {
	switch s = strings.ToLower(s); {
	case strings.HasPrefix(s, "d"):
		return 24 * time.Hour
	case strings.HasPrefix(s, "h"):
		return time.Hour
	case strings.HasPrefix(s, "m"):
		return time.Minute
	default:
		return time.Second
	}
}

func durParseISO(s string) (time.Duration, bool) {
	m := durISORE.FindStringSubmatch(s)
	if m == nil || s == "P" || strings.EqualFold(s, "PT") {
		return 0, false
	}
	var total time.Duration
	for i, unit := range []time.Duration{24 * time.Hour, time.Hour, time.Minute, time.Second} {
		if m[i+1] == "" {
			continue
		}
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return 0, false
		}
		total += time.Duration(n) * unit
	}
	return total, true
}
