package quantity

import (
	"errors"
	"fmt"
	"strings"
)

// The stored forms of quantities and unit references, shared by every package
// that persists them (the recipe store and the shopping store). Rows already
// on disk were written in these forms, so they must not change: a quantity is
// absent for none, "n/d" for an exact value and "n/d..n/d" for a range, with
// n/d the exact reduced fraction; a unit is a reference by integer id.
//
// The codec is pure. Callers own the database types and map the errors below
// onto their own sentinels.

// rangeSep separates the bounds of a stored range.
const rangeSep = ".."

// ErrNegativeStored is returned by EncodeQty for a negative bound, which
// could not be read back.
var ErrNegativeStored = errors.New("negative quantity")

// ErrUnknownUnit is returned by a UnitTable for a unit or id it does not hold.
var ErrUnknownUnit = errors.New("unknown unit")

// EncodeRat returns r as the exact reduced fraction "n/d", which ParseRat
// reads back without loss.
func EncodeRat(r Rat) string {
	return r.Num().String() + "/" + r.Denom().String()
}

// EncodeQty returns the stored text of q and whether there is any: none for an
// absent quantity, "n/d" for an exact value, "n/d..n/d" for a range. A
// negative bound is refused with ErrNegativeStored rather than failing at the
// next read.
func EncodeQty(q Quantity) (string, bool, error) {
	if q.IsAbsent() {
		return "", false, nil
	}
	if q.Min().Sign() < 0 || q.Max().Sign() < 0 {
		return "", false, fmt.Errorf("%w %s", ErrNegativeStored, q)
	}
	s := EncodeRat(q.Min())
	if q.IsRange() {
		s += rangeSep + EncodeRat(q.Max())
	}
	return s, true, nil
}

// DecodeQty is the inverse of EncodeQty for stored text; an absent quantity
// has no text and so is the caller's to handle.
func DecodeQty(s string) (Quantity, error) {
	lo, hi, isRange := strings.Cut(s, rangeSep)
	loR, err := ParseRat(lo)
	if err != nil {
		return Quantity{}, fmt.Errorf("quantity %q: %w", s, err)
	}
	if !isRange {
		return Exact(loR), nil
	}
	hiR, err := ParseRat(hi)
	if err != nil {
		return Quantity{}, fmt.Errorf("quantity %q: %w", s, err)
	}
	q, err := NewRange(loR, hiR)
	if err != nil {
		return Quantity{}, fmt.Errorf("quantity %q: %w", s, err)
	}
	return q, nil
}

// UnitTable maps units to the integer ids a database stores for them and back.
// The zero Unit is id 0, which a caller stores as NULL (or as 0 in a document).
type UnitTable struct {
	ids   map[string]int64
	units map[int64]Unit
}

// NewUnitTable returns an empty table.
func NewUnitTable() *UnitTable {
	return &UnitTable{ids: map[string]int64{}, units: map[int64]Unit{}}
}

// UnitTableOver returns a table backed by the given maps, without copying.
func UnitTableOver(ids map[string]int64, units map[int64]Unit) *UnitTable {
	return &UnitTable{ids: ids, units: units}
}

// Add records that u is stored as id.
func (t *UnitTable) Add(id int64, u Unit) {
	t.ids[u.Name] = id
	t.units[id] = u
}

// ID returns the id stored for u: 0 for the zero Unit, ErrUnknownUnit when u
// is not in the table.
func (t *UnitTable) ID(u Unit) (int64, error) {
	if u.IsZero() {
		return 0, nil
	}
	id, ok := t.ids[u.Name]
	if !ok {
		return 0, fmt.Errorf("unit %q: %w", u.Name, ErrUnknownUnit)
	}
	return id, nil
}

// Unit resolves a stored id: the zero Unit for 0, ErrUnknownUnit when the id
// is not in the table.
func (t *UnitTable) Unit(id int64) (Unit, error) {
	if id == 0 {
		return Unit{}, nil
	}
	u, ok := t.units[id]
	if !ok {
		return Unit{}, fmt.Errorf("unit id %d: %w", id, ErrUnknownUnit)
	}
	return u, nil
}
