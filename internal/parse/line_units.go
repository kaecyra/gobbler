package parse

import (
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// UnitMatcher resolves a unit spelling (lowercase, trailing dot removed) to the
// catalog's unit. The catalog owns unit names and aliases; this package keeps
// no table of its own. Implementations must be deterministic and should answer
// from memory, like IngredientMatcher. A nil UnitMatcher recognises no unit.
type UnitMatcher interface {
	MatchUnit(name string) (quantity.Unit, bool)
}

// lineUnits reads units through a UnitMatcher.
type lineUnits struct{ m UnitMatcher }

// lookup resolves a unit spelling such as "Tbsp.", "T" or "fl oz". Spellings
// are lowercased and lose a trailing dot before the matcher sees them, except
// the single letters "T" and "t", which differ only by case: the catalog
// lowercases aliases, so they are mapped to "tbsp" and "tsp" here.
func (r lineUnits) lookup(s string) (quantity.Unit, bool) {
	if r.m == nil {
		return quantity.Unit{}, false
	}
	switch s {
	case "T":
		return r.m.MatchUnit("tbsp")
	case "t":
		return r.m.MatchUnit("tsp")
	}
	s = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
	s = strings.ReplaceAll(s, "fl. oz", "fl oz")
	if s == "" {
		return quantity.Unit{}, false
	}
	return r.m.MatchUnit(s)
}

// at reads a unit from the front of toks: two words ("fl oz") are tried
// before one. n is the number of tokens used.
func (r lineUnits) at(toks []string) (u quantity.Unit, n int) {
	if len(toks) >= 2 {
		if u, ok := r.lookup(toks[0] + " " + toks[1]); ok && !lineIsParen(toks[0]) {
			return u, 2
		}
	}
	if len(toks) >= 1 && !lineIsParen(toks[0]) {
		if u, ok := r.lookup(toks[0]); ok {
			return u, 1
		}
	}
	return quantity.Unit{}, 0
}
