package ingest

import "github.com/kaecyra/gobbler/internal/quantity"

// MatchUnit implements parse.UnitMatcher. The catalog is the one owner of unit
// names and aliases, so the line parser asks it for every unit spelling and
// only ever returns catalog units; a word the catalog does not know stays in
// the ingredient name and costs the line confidence.
func (r *resolver) MatchUnit(name string) (quantity.Unit, bool) {
	u, ok := r.unit(quantity.OtherUnit(name))
	if !ok {
		return quantity.Unit{}, false
	}
	return u, true
}
