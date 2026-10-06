package quantity

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidRange is returned when a range's minimum exceeds its maximum.
var ErrInvalidRange = errors.New("range minimum exceeds maximum")

// Kind says which shape a Quantity has.
type Kind int

const (
	// KindAbsent is "salt to taste": no number at all. It is the zero value.
	KindAbsent Kind = iota
	// KindExact is a single rational.
	KindExact
	// KindRange is a min-max pair of rationals.
	KindRange
)

// Quantity is an absent value, an exact rational, or a range. The zero value
// is absent. Quantities are immutable.
type Quantity struct {
	kind     Kind
	min, max Rat
}

// Absent returns the quantity of a line that has none ("salt to taste").
func Absent() Quantity { return Quantity{} }

// Exact returns the quantity r.
func Exact(r Rat) Quantity { return Quantity{kind: KindExact, min: r, max: r} }

// NewRange returns the quantity lo to hi, or ErrInvalidRange when lo > hi.
func NewRange(lo, hi Rat) (Quantity, error) {
	if lo.Cmp(hi) > 0 {
		return Quantity{}, fmt.Errorf("%w: %s > %s", ErrInvalidRange, lo, hi)
	}
	return Quantity{kind: KindRange, min: lo, max: hi}, nil
}

// Kind reports the shape of q.
func (q Quantity) Kind() Kind { return q.kind }

// IsAbsent reports whether q has no number.
func (q Quantity) IsAbsent() bool { return q.kind == KindAbsent }

// IsRange reports whether q is a range.
func (q Quantity) IsRange() bool { return q.kind == KindRange }

// Min returns the exact value, or the lower bound of a range. It is zero for
// an absent quantity.
func (q Quantity) Min() Rat { return q.min }

// Max returns the exact value, or the upper bound of a range. It is zero for
// an absent quantity.
func (q Quantity) Max() Rat { return q.max }

// Equal reports whether q and o have the same kind and bounds.
func (q Quantity) Equal(o Quantity) bool {
	return q.kind == o.kind && q.min.Equal(o.min) && q.max.Equal(o.max)
}

// Mul multiplies both bounds by r. A zero or negative r is an error
// (ErrInvalidScale): it would zero the quantity or invert a range. An absent
// quantity is returned unchanged.
func (q Quantity) Mul(r Rat) (Quantity, error) {
	if r.Sign() <= 0 {
		return Quantity{}, fmt.Errorf("%w: multiplier %s", ErrInvalidScale, r)
	}
	if q.IsAbsent() {
		return q, nil
	}
	return Quantity{kind: q.kind, min: q.min.Mul(r), max: q.max.Mul(r)}, nil
}

// Div divides both bounds by r. Zero is ErrDivideByZero and a negative r is
// ErrInvalidScale. An absent quantity is returned unchanged.
func (q Quantity) Div(r Rat) (Quantity, error) {
	if r.Sign() == 0 {
		return Quantity{}, ErrDivideByZero
	}
	if r.Sign() < 0 {
		return Quantity{}, fmt.Errorf("%w: divisor %s", ErrInvalidScale, r)
	}
	if q.IsAbsent() {
		return q, nil
	}
	lo, err := q.min.Div(r)
	if err != nil {
		return Quantity{}, err
	}
	hi, err := q.max.Div(r)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{kind: q.kind, min: lo, max: hi}, nil
}

// String renders fractions: "", "1 1/2", "2-3".
func (q Quantity) String() string {
	return q.render(Rat.String)
}

// Decimal renders decimals with at most places digits: "", "2.5", "2-3".
func (q Quantity) Decimal(places int) string {
	return q.render(func(r Rat) string { return r.Decimal(places) })
}

func (q Quantity) render(f func(Rat) string) string {
	switch q.kind {
	case KindExact:
		return f(q.min)
	case KindRange:
		return f(q.min) + "-" + f(q.max)
	}
	return ""
}

// PackageSize is the size of one package: the "14 oz" of "1 (14 oz) can".
type PackageSize struct {
	Qty  Quantity
	Unit Unit
}

// Amount is a quantity with its unit and, for packaged goods, the size of one
// package: "1 (14 oz) can" is Qty 1, Unit can, Package 14 oz. Amounts are
// plain values with no shared pointers, so every function returns a new one
// and cannot change its input.
type Amount struct {
	Qty  Quantity
	Unit Unit
	// Package is the size of one unit, kept so shopping can total it; it is
	// meaningful only when HasPackage is set. It is never scaled or
	// converted: scaling changes how many packages, not how big each one is.
	Package    PackageSize
	HasPackage bool
}

// String renders the amount as written: "1 1/2 cup", "1 (14 oz) can",
// "250 g". Metric units render as decimals, others as fractions.
func (a Amount) String() string {
	var parts []string
	if a.Unit.System == SystemMetric {
		parts = append(parts, a.Qty.Decimal(2))
	} else {
		parts = append(parts, a.Qty.String())
	}
	if a.HasPackage {
		parts = append(parts, "("+Amount{Qty: a.Package.Qty, Unit: a.Package.Unit}.String()+")")
	}
	parts = append(parts, a.Unit.Name)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func (a Amount) withQty(q Quantity) Amount {
	a.Qty = q
	return a
}
