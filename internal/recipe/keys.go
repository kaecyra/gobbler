package recipe

import "strings"

// Prefixes of the keys that name a step and an ingredient line across versions
// of a recipe (ADR-00035). A key is its prefix and KeyRandomLen characters of
// the lowercase Crockford base32 alphabet, KeyAlphabet. The prefix makes a
// step key passed where a line key belongs fail visibly.
const (
	StepKeyPrefix = "stp-"
	LineKeyPrefix = "lin-"
	KeyRandomLen  = 8
	// KeyAlphabet is lowercase Crockford base32: digits and letters without
	// i, l, o and u.
	KeyAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"
)

// IsStepKey reports whether id is shaped like a stored step's key. Any other
// ID is a document-local label (see StepID).
func IsStepKey(id StepID) bool { return hasKeyShape(string(id), StepKeyPrefix) }

// IsLineKey reports whether k is shaped like a stored ingredient line's key.
func IsLineKey(k string) bool { return hasKeyShape(k, LineKeyPrefix) }

func hasKeyShape(s, prefix string) bool {
	rest, ok := strings.CutPrefix(s, prefix)
	if !ok || len(rest) != KeyRandomLen {
		return false
	}
	for _, c := range rest {
		if !strings.ContainsRune(KeyAlphabet, c) {
			return false
		}
	}
	return true
}
