package shopping

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// Text renders the list as plain text for copying anywhere: a heading per
// aisle, then one line per item, "- name: amount + amount". Amounts are
// rounded to kitchen precision (quantity.Round); the list itself keeps the
// exact values. Dropped pantry items are not included. An empty list renders
// as the empty string.
func (l List) Text() (string, error) {
	var b strings.Builder
	for i, a := range l.Aisles {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(titleCase(a.Name))
		b.WriteByte('\n')
		for _, it := range a.Items {
			line, err := itemLine(it)
			if err != nil {
				return "", err
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

func itemLine(it Item) (string, error) {
	parts := make([]string, 0, len(it.Amounts))
	for _, a := range it.Amounts {
		r, err := quantity.Round(a)
		if err != nil {
			return "", fmt.Errorf("round amount for %q: %w", it.Name, err)
		}
		parts = append(parts, r.Rounded.String())
	}
	if len(parts) == 0 {
		return "- " + it.Name, nil
	}
	return "- " + it.Name + ": " + strings.Join(parts, " + "), nil
}

func titleCase(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
