package quantity

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode/utf8"
)

// ErrMalformed is returned when text cannot be read as a quantity.
var ErrMalformed = errors.New("malformed quantity")

// unicodeFractions maps vulgar fraction runes to their exact value.
var unicodeFractions = map[rune][2]int64{
	'½': {1, 2}, '⅓': {1, 3}, '⅔': {2, 3}, '¼': {1, 4}, '¾': {3, 4},
	'⅕': {1, 5}, '⅖': {2, 5}, '⅗': {3, 5}, '⅘': {4, 5},
	'⅙': {1, 6}, '⅚': {5, 6}, '⅐': {1, 7},
	'⅛': {1, 8}, '⅜': {3, 8}, '⅝': {5, 8}, '⅞': {7, 8},
	'⅑': {1, 9}, '⅒': {1, 10},
}

// ParseRat reads a single non-negative number: a whole number ("2"), decimal
// ("0.5", ".5"), fraction ("3/4"), unicode fraction ("⅓"), or mixed number
// ("1 1/2", "1-1/2", "1½", "1 ½").
func ParseRat(s string) (Rat, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "⁄", "/")) // fraction slash
	if s == "" {
		return Rat{}, fmt.Errorf("%w: empty", ErrMalformed)
	}

	if last, size := utf8.DecodeLastRuneInString(s); size > 0 {
		if f, ok := unicodeFractions[last]; ok {
			return parseWholeAndFraction(strings.TrimSpace(s[:len(s)-size]), big.NewRat(f[0], f[1]), s)
		}
	}

	if fields := strings.Fields(s); len(fields) == 2 {
		frac, err := parseSimple(fields[1])
		if err != nil || !strings.Contains(fields[1], "/") {
			return Rat{}, fmt.Errorf("%w: %q", ErrMalformed, s)
		}
		return parseWholeAndFraction(fields[0], frac, s)
	} else if len(fields) > 2 {
		return Rat{}, fmt.Errorf("%w: %q", ErrMalformed, s)
	}

	if whole, frac, ok := strings.Cut(s, "-"); ok && whole != "" && strings.Contains(frac, "/") {
		f, err := parseSimple(frac)
		if err != nil {
			return Rat{}, fmt.Errorf("%w: %q", ErrMalformed, s)
		}
		return parseWholeAndFraction(whole, f, s)
	}

	r, err := parseSimple(s)
	if err != nil {
		return Rat{}, fmt.Errorf("%q: %w", s, err)
	}
	return Rat{v: r}, nil
}

// parseWholeAndFraction adds a whole-number prefix (possibly empty) to a
// fraction that must be proper. orig is the full input, for messages.
func parseWholeAndFraction(whole string, frac *big.Rat, orig string) (Rat, error) {
	if whole != "" && frac.Cmp(big.NewRat(1, 1)) >= 0 {
		return Rat{}, fmt.Errorf("%w: %q", ErrMalformed, orig)
	}
	if whole == "" {
		return Rat{v: frac}, nil
	}
	if !allDigits(whole) {
		return Rat{}, fmt.Errorf("%w: %q", ErrMalformed, orig)
	}
	w, ok := new(big.Rat).SetString(whole)
	if !ok {
		return Rat{}, fmt.Errorf("%w: %q", ErrMalformed, orig)
	}
	return Rat{v: w.Add(w, frac)}, nil
}

// parseSimple reads digits, a decimal or a fraction a/b.
func parseSimple(s string) (*big.Rat, error) {
	switch {
	case allDigits(s):
		r, _ := new(big.Rat).SetString(s)
		return r, nil
	case strings.Contains(s, "/"):
		n, d, _ := strings.Cut(s, "/")
		if !allDigits(n) || !allDigits(d) {
			return nil, ErrMalformed
		}
		num, _ := new(big.Int).SetString(n, 10)
		den, _ := new(big.Int).SetString(d, 10)
		if den.Sign() == 0 {
			return nil, ErrDivideByZero
		}
		return new(big.Rat).SetFrac(num, den), nil
	case strings.Contains(s, "."):
		l, r, _ := strings.Cut(s, ".")
		if (l != "" && !allDigits(l)) || !allDigits(r) {
			return nil, ErrMalformed
		}
		v, ok := new(big.Rat).SetString("0" + s)
		if !ok {
			return nil, ErrMalformed
		}
		return v, nil
	}
	return nil, ErrMalformed
}

// allDigits reports whether s is a non-empty run of ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ParseQuantity reads an exact value or a range: "1 1/2", "⅓", "2-3",
// "2–3", "1/2 to 3/4". Empty or unreadable input is an error; an absent
// quantity is constructed with Absent, not parsed.
func ParseQuantity(s string) (Quantity, error) {
	s = strings.TrimSpace(s)
	if r, err := ParseRat(s); err == nil {
		return Exact(r), nil
	}
	for _, sep := range []string{"–", "—", "-", " to "} {
		for i := 0; i < len(s); i++ {
			if !strings.HasPrefix(s[i:], sep) {
				continue
			}
			lo, errLo := ParseRat(s[:i])
			hi, errHi := ParseRat(s[i+len(sep):])
			if errLo != nil || errHi != nil {
				continue
			}
			return NewRange(lo, hi)
		}
	}
	return Quantity{}, fmt.Errorf("%w: %q", ErrMalformed, s)
}
