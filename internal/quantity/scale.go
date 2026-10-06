package quantity

import (
	"errors"
	"fmt"
)

// ErrInvalidScale is returned for a zero or negative multiplier or serving
// count.
var ErrInvalidScale = errors.New("scale must be positive")

// ErrUnknownRule is returned for a scaling rule other than linear, fixed or
// to_taste.
var ErrUnknownRule = errors.New("unknown scaling rule")

// Rule says how an ingredient line responds to scaling (ADR-00005).
type Rule string

// Scaling rules. RuleLinear is the default.
const (
	// RuleLinear multiplies the quantity.
	RuleLinear Rule = "linear"
	// RuleFixed leaves the line unchanged ("1 bay leaf").
	RuleFixed Rule = "fixed"
	// RuleToTaste has no quantity and is unchanged.
	RuleToTaste Rule = "to_taste"
)

// ParseRule validates a rule name and is the entry point for stored or
// user-supplied text: only here does the empty string mean RuleLinear.
func ParseRule(s string) (Rule, error) {
	switch r := Rule(s); r {
	case "":
		return RuleLinear, nil
	case RuleLinear, RuleFixed, RuleToTaste:
		return r, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownRule, s)
}

// ServingsFactor returns the multiplier that takes a recipe written for base
// servings to target servings. Both must be positive.
func ServingsFactor(base, target Rat) (Rat, error) {
	if base.Sign() <= 0 || target.Sign() <= 0 {
		return Rat{}, fmt.Errorf("%w: servings %s to %s", ErrInvalidScale, base, target)
	}
	return target.Div(base)
}

// Scale applies rule and multiplier to a. A linear amount is multiplied and
// then moved to a more readable unit where one exists (48 tsp becomes 1 cup);
// fixed and to-taste amounts are returned unchanged for any multiplier. The
// package size is never scaled. A non-positive multiplier is an error, and so
// is any rule other than the three constants (including the empty string; use
// ParseRule to read stored text), which wraps ErrUnknownRule.
func Scale(a Amount, rule Rule, mult Rat) (Amount, error) {
	switch rule {
	case RuleLinear, RuleFixed, RuleToTaste:
	default:
		return Amount{}, fmt.Errorf("%w: %q", ErrUnknownRule, string(rule))
	}
	if mult.Sign() <= 0 {
		return Amount{}, fmt.Errorf("%w: multiplier %s", ErrInvalidScale, mult)
	}
	if rule != RuleLinear || a.Qty.IsAbsent() {
		return a, nil
	}
	return Readable(a.withQty(a.Qty.Mul(mult))), nil
}

// rung is one step of a unit ladder. thirds allows a quantity with a
// denominator of 3 in this unit (1/3 cup is natural, 1/3 tbsp is not).
type rung struct {
	unit   string
	thirds bool
}

// ladders lists, smallest first, the units Readable may move up through
// within one system and dimension.
var ladders = [][]rung{
	{{"tsp", false}, {"tbsp", false}, {"cup", true}},
	{{"oz", false}, {"lb", false}},
	{{"ml", false}, {"l", false}},
	{{"g", false}, {"kg", false}},
}

// Readable moves a to the largest larger unit in the same system in which
// every bound is at least 1 and a common fraction (whole, half, quarter,
// eighth, and thirds for cups). It never moves to a smaller unit and returns a
// unchanged when no such unit exists.
func Readable(a Amount) Amount {
	if a.Qty.IsAbsent() {
		return a
	}
	for _, ladder := range ladders {
		at := -1
		for i, r := range ladder {
			if r.unit == a.Unit.Name {
				at = i
			}
		}
		if at < 0 {
			continue
		}
		for j := len(ladder) - 1; j > at; j-- {
			cand, err := Convert(a, MustUnit(ladder[j].unit))
			if err != nil { // unreachable: ladder units share a dimension
				return a
			}
			if cand.Qty.Min().Cmp(Int(1)) >= 0 && common(cand.Qty.Min(), ladder[j].thirds) && common(cand.Qty.Max(), ladder[j].thirds) {
				return cand
			}
		}
	}
	return a
}

// common reports whether r's denominator is 1, 2, 4 or 8 (or 3 when thirds).
func common(r Rat, thirds bool) bool {
	d := r.Denom()
	if !d.IsInt64() {
		return false
	}
	switch d.Int64() {
	case 1, 2, 4, 8:
		return true
	case 3:
		return thirds
	}
	return false
}
