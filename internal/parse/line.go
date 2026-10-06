package parse

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// IngredientMatch is a catalog ingredient a line's name resolved to.
type IngredientMatch struct {
	// ID is the catalog's identifier for the ingredient.
	ID int64
	// Name is the catalog's canonical name.
	Name string
}

// IngredientMatcher resolves an ingredient name (lowercase, qualifiers
// removed) to a catalog ingredient. The catalog satisfies it at the call
// site; tests use a fake. Implementations must be deterministic and should
// answer from memory: the parser is pure and calls this once or twice per
// line.
type IngredientMatcher interface {
	MatchIngredient(name string) (IngredientMatch, bool)
}

// LineQualifier is one qualifier from an ingredient line: "melted" on
// "butter, melted".
type LineQualifier struct {
	Kind LineQualifierKind
	Text string
}

// LineResult is the structured reading of one ingredient line.
type LineResult struct {
	// Raw is the input exactly as given.
	Raw string
	// Amount holds quantity (exact, range or absent), unit (zero for "2
	// eggs") and, for "1 (14 oz) can", the package size.
	Amount quantity.Amount
	// ToTaste marks "salt to taste": no quantity is needed.
	ToTaste bool
	// Optional marks "(optional)" and similar markers.
	Optional bool
	// Name is the ingredient name without qualifiers, lowercase.
	Name string
	// Match is the catalog ingredient Name resolved to; valid when Matched.
	Match   IngredientMatch
	Matched bool
	// Qualifiers are in reading order: those leading the name, then
	// parenthetical notes, then those after a comma.
	Qualifiers []LineQualifier
	// Confidence is 0 to 1, rounded to two places. Unrecognised units,
	// unmatched ingredients, unreadable quantities and leftover text lower it.
	Confidence float64
	// Issues says why Confidence is below 1, for the review screen.
	Issues []string
}

// Confidence penalties. Each is one reason, subtracted from 1.
const (
	linePenaltyUnmatched = 0.3
	linePenaltyUnit      = 0.3
	linePenaltyQuantity  = 0.4
	linePenaltyLeftover  = 0.05
	linePenaltyNoAmount  = 0.1
	lineNoNameConfidence = 0.1
)

var (
	lineBulletRE   = regexp.MustCompile(`^[\s\-•*·▢☐◦▪–—]+`)
	lineOptLeadRE  = regexp.MustCompile(`(?i)\(\s*optional\s*[,;]\s*`)
	lineOptTailRE  = regexp.MustCompile(`(?i)\s*[,;]\s*optional\s*\)`)
	lineOptBareRE  = regexp.MustCompile(`(?i)[,;]?\s*\boptional\b`)
	lineTasteRE    = regexp.MustCompile(`(?i)\(\s*to taste\s*\)|[,;]?\s*\bto taste\b\.?`)
	lineEmptyRE    = regexp.MustCompile(`\(\s*\)|\[\s*\]`)
	lineAttachedRE = regexp.MustCompile(`^([\d./⁄½¼¾⅓⅔⅕⅖⅗⅘⅙⅚⅐⅛⅜⅝⅞⅑⅒]+)-?([A-Za-z]+\.?)$`)
)

// IngredientLine reads one ingredient line. It never fails: a line it cannot fully
// read comes back with a lower Confidence and Issues saying why. m may be
// nil, in which case no catalog matching is attempted and an unmatched name
// costs no confidence.
func IngredientLine(raw string, m IngredientMatcher) LineResult {
	res := LineResult{Raw: raw}
	s := strings.Join(strings.Fields(raw), " ")
	s = lineBulletRE.ReplaceAllString(s, "")
	if s == "" {
		res.Issues = []string{"empty line"}
		return res
	}

	s, res.Optional = lineStripOptional(s)
	if loc := lineTasteRE.FindStringIndex(s); loc != nil {
		res.ToTaste = true
		s = s[:loc[0]] + s[loc[1]:]
	}
	s = lineTidy(s)

	pieces := lineSplitTop(s, ",")
	head := strings.TrimSpace(pieces[0])
	tail := strings.TrimSpace(strings.Join(pieces[1:], ","))

	p := lineParser{res: &res, m: m}
	p.parseHead(head)
	p.parseTail(tail)
	p.score()
	return res
}

func lineStripOptional(s string) (string, bool) {
	before := s
	s = lineOptLeadRE.ReplaceAllString(s, "(")
	s = lineOptTailRE.ReplaceAllString(s, ")")
	s = lineOptBareRE.ReplaceAllString(s, " ")
	return s, s != before
}

// lineTidy removes the debris left by stripping markers.
func lineTidy(s string) string {
	s = lineEmptyRE.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, " ,", ",")
	// A leading dot is kept: ".25 tsp" is a quantity.
	return strings.TrimRight(strings.Trim(s, " ,;:"), " ,;:.")
}

type lineParser struct {
	res *LineResult
	m   IngredientMatcher
	// unitGuess is a word that looks like an unrecognised unit.
	unitGuess string
	leftover  int
	// qtyUnreadable is set when the line starts like a number we could not read.
	qtyUnreadable bool
}

func (p *lineParser) issue(format string, args ...any) {
	p.res.Issues = append(p.res.Issues, fmt.Sprintf(format, args...))
}

func (p *lineParser) addQualifier(k LineQualifierKind, text string) {
	p.res.Qualifiers = append(p.res.Qualifiers, LineQualifier{Kind: k, Text: text})
}

func (p *lineParser) parseHead(head string) {
	toks := lineTokens(head)
	// "about 2 cups": the hedge is a note, not part of the amount.
	for len(toks) > 0 && lineIsHedge(toks[0]) {
		p.addQualifier(LineQualNote, strings.ToLower(strings.TrimPrefix(toks[0], "~")))
		toks = toks[1:]
	}
	toks = lineSplitAttached(toks)

	qty, used := lineLeadingQuantity(toks)
	if used > 0 && used < len(toks) && lineLooksNumeric(toks[used]) {
		// "1 3/4/2 cups": a number follows the quantity we read, so we did
		// not read all of it. Keep nothing rather than a wrong number.
		qty, used = quantity.Absent(), 0
	}
	toks = toks[used:]
	// "a pinch", "an onion": the article is the number one when a unit follows.
	if used == 0 && len(toks) > 1 {
		if w := strings.ToLower(toks[0]); w == "a" || w == "an" {
			if _, n := lineUnitAt(toks[1:]); n > 0 {
				qty, toks = quantity.Exact(quantity.Int(1)), toks[1:]
				used = 1
			}
		}
	}
	if used == 0 && len(toks) > 0 && lineLooksNumeric(toks[0]) {
		p.qtyUnreadable = true
		p.issue("unreadable quantity %q", toks[0])
	}
	p.res.Amount.Qty = qty

	// "1 (14 oz) can": a package size sits between quantity and unit.
	if len(toks) > 0 && lineIsParen(toks[0]) && used > 0 {
		if pkg, ok := lineParsePackage(toks[0]); ok {
			p.res.Amount.Package, p.res.Amount.HasPackage = pkg, true
			toks = toks[1:]
		}
	}

	if u, n := lineUnitAt(toks); n > 0 && (used > 0 || u.Dimension == quantity.DimOther) {
		p.res.Amount.Unit = u
		toks = toks[n:]
		if len(toks) > 1 && strings.EqualFold(toks[0], "of") {
			toks = toks[1:]
		}
		// "1 can (14 oz) tomatoes": the package size follows its unit.
		if len(toks) > 0 && lineIsParen(toks[0]) && u.Dimension == quantity.DimOther && !p.res.Amount.HasPackage {
			if pkg, ok := lineParsePackage(toks[0]); ok {
				p.res.Amount.Package, p.res.Amount.HasPackage = pkg, true
				toks = toks[1:]
			}
		}
	}

	var words []string
	var notes []string
	for _, t := range toks {
		if lineIsParen(t) {
			notes = append(notes, strings.TrimSpace(strings.Trim(t, "()[]")))
			continue
		}
		words = append(words, t)
	}
	p.resolveName(words, used > 0 && p.res.Amount.Unit.IsZero(), notes)
}

// resolveName separates leading qualifier words from the ingredient name and
// matches the name. countOnly is true when a quantity but no unit was read,
// so the first word might be an unrecognised unit.
func (p *lineParser) resolveName(words []string, countOnly bool, notes []string) {
	for i, w := range words {
		words[i] = strings.ToLower(strings.Trim(w, ".;:"))
	}
	full := strings.TrimSpace(strings.TrimPrefix(strings.Join(words, " "), "of "))

	// A catalog entry that includes the qualifier word ("ground beef") wins.
	if m, ok := p.match(full); ok {
		p.res.Name, p.res.Match, p.res.Matched = full, m, true
		p.addNotes(notes)
		return
	}

	var lead []LineQualifier
	rest := words
	for len(rest) > 1 {
		k, text, n := lineLeadingQualifier(rest)
		if n == 0 || n >= len(rest) {
			break
		}
		lead = append(lead, LineQualifier{Kind: k, Text: text})
		rest = rest[n:]
	}
	p.res.Qualifiers = append(p.res.Qualifiers, lead...)
	name := strings.TrimSpace(strings.TrimPrefix(strings.Join(rest, " "), "of "))
	p.res.Name = name
	p.addNotes(notes)

	if m, ok := p.match(name); ok {
		p.res.Match, p.res.Matched = m, true
		return
	}
	// "2 cupz flour": a count line whose first word is not a name but whose
	// remainder is one probably has an unrecognised unit. The name is left
	// as written ("2 sweet potatoes" must not become "potatoes"); the
	// guess only adds an issue and a penalty on top of the unmatched name.
	if countOnly && p.m != nil && len(rest) > 1 {
		if _, ok := p.match(strings.Join(rest[1:], " ")); ok {
			p.unitGuess = rest[0]
		}
	}
}

func (p *lineParser) addNotes(notes []string) {
	for _, n := range notes {
		if n == "" {
			continue
		}
		p.addQualifier(LineQualNote, strings.ToLower(n))
	}
}

func (p *lineParser) match(name string) (IngredientMatch, bool) {
	if p.m == nil || name == "" {
		return IngredientMatch{}, false
	}
	return p.m.MatchIngredient(name)
}

func (p *lineParser) parseTail(tail string) {
	for _, seg := range lineSplitSegments(tail) {
		kind, ok := lineClassifySegment(seg)
		if !ok {
			p.leftover++
			p.issue("unrecognised text %q", seg)
		}
		p.addQualifier(kind, strings.ToLower(strings.Trim(seg, ".")))
	}
}

func (p *lineParser) score() {
	r := p.res
	if r.Name == "" {
		r.Issues = append(r.Issues, "no ingredient name")
		r.Confidence = lineNoNameConfidence
		return
	}
	c := 1.0
	if p.m != nil && !r.Matched {
		c -= linePenaltyUnmatched
		p.issue("ingredient %q not matched", r.Name)
	}
	if p.unitGuess != "" {
		c -= linePenaltyUnit
		p.issue("unrecognised unit %q", p.unitGuess)
	}
	if p.qtyUnreadable {
		c -= linePenaltyQuantity
	}
	c -= linePenaltyLeftover * float64(p.leftover)
	if r.Amount.Qty.IsAbsent() && r.Amount.Unit.IsZero() && !r.ToTaste && !p.qtyUnreadable {
		c -= linePenaltyNoAmount
		p.issue("no quantity")
	}
	r.Confidence = math.Round(math.Max(c, 0)*100) / 100
}

// lineTokens splits on spaces outside parentheses and brackets, so a
// parenthetical stays one token.
func lineTokens(s string) []string {
	var out []string
	var cur strings.Builder
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '(' || r == '[':
			depth++
		case (r == ')' || r == ']') && depth > 0:
			depth--
		case r == ' ' && depth == 0:
			flush()
			continue
		}
		cur.WriteRune(r)
	}
	flush()
	return out
}

func lineIsParen(t string) bool { return strings.HasPrefix(t, "(") || strings.HasPrefix(t, "[") }

func lineIsHedge(t string) bool {
	switch strings.ToLower(t) {
	case "about", "approx", "approx.", "approximately", "~", "around":
		return true
	}
	return false
}

func lineLooksNumeric(t string) bool {
	for _, r := range t {
		return unicode.IsDigit(r) || (r >= '½' && r <= '⅞') || r == '.' || strings.ContainsRune("⅐⅑⅒", r)
	}
	return false
}

// lineSplitAttached turns "200g" and "8-ounce" into two tokens when the
// letters are a known unit, so quantity and unit are read the usual way.
func lineSplitAttached(toks []string) []string {
	out := make([]string, 0, len(toks)+1)
	for _, t := range toks {
		if m := lineAttachedRE.FindStringSubmatch(t); m != nil {
			if _, ok := lineLookupUnit(m[2]); ok {
				out = append(out, m[1], m[2])
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// lineMaxQuantityTokens bounds the leading run tried as a quantity: "1 1/2 to
// 2 1/2" is five tokens.
const lineMaxQuantityTokens = 7

// lineLeadingQuantity reads the longest leading run of tokens that
// quantity.ParseQuantity accepts and returns it with the token count.
func lineLeadingQuantity(toks []string) (quantity.Quantity, int) {
	limit := min(lineMaxQuantityTokens, len(toks))
	for n := limit; n > 0; n-- {
		if lineIsParen(toks[n-1]) {
			continue
		}
		if !lineLooksNumeric(toks[0]) {
			break
		}
		if q, err := quantity.ParseQuantity(strings.Join(toks[:n], " ")); err == nil {
			return q, n
		}
	}
	return quantity.Absent(), 0
}

// lineUnitAt reads a unit from the front of toks: two words ("fl oz") are
// tried before one. n is the number of tokens used.
func lineUnitAt(toks []string) (u quantity.Unit, n int) {
	if len(toks) >= 2 {
		if u, ok := lineLookupUnit(toks[0] + " " + toks[1]); ok && !lineIsParen(toks[0]) {
			return u, 2
		}
	}
	if len(toks) >= 1 && !lineIsParen(toks[0]) {
		if u, ok := lineLookupUnit(toks[0]); ok {
			return u, 1
		}
	}
	return quantity.Unit{}, 0
}

// lineParsePackage reads "(14 oz)", "(14-ounce)" or "(8 oz.)" as a package
// size. Anything else in parentheses is a note, not a size.
func lineParsePackage(tok string) (quantity.PackageSize, bool) {
	inner := strings.TrimSpace(strings.Trim(tok, "()[]"))
	toks := lineSplitAttached(strings.Fields(inner))
	qty, used := lineLeadingQuantity(toks)
	if used == 0 || used != len(toks)-1 {
		return quantity.PackageSize{}, false
	}
	u, ok := lineLookupUnit(toks[used])
	if !ok || !u.Converts() {
		return quantity.PackageSize{}, false
	}
	return quantity.PackageSize{Qty: qty, Unit: u}, true
}
