package quantity

import (
	"errors"
	"fmt"
)

// ErrNotConvertible is returned when two units cannot be converted: different
// dimensions, or a unit in the "other" dimension.
var ErrNotConvertible = errors.New("units are not convertible")

// Convert expresses a in unit to. Conversion within mass, volume or count is
// exact. Units in DimOther, and units of different dimensions (including
// volume to mass, which needs ToMass and a density), return ErrNotConvertible.
// Converting to the same unit always succeeds. An absent quantity is returned
// with its unit unchanged. The package size is carried over untouched.
func Convert(a Amount, to Unit) (Amount, error) {
	if a.Unit.Name == to.Name && a.Unit.Dimension == to.Dimension {
		return a, nil
	}
	if !a.Unit.Converts() || !to.Converts() || a.Unit.Dimension != to.Dimension {
		return Amount{}, fmt.Errorf("%w: %q (%s) to %q (%s)", ErrNotConvertible,
			a.Unit.Name, a.Unit.Dimension, to.Name, to.Dimension)
	}
	if a.Qty.IsAbsent() {
		return a, nil
	}
	// Factors are positive constants, so the division cannot fail.
	ratio, _ := a.Unit.Factor.Div(to.Factor)
	out := a.withQty(a.Qty.Mul(ratio))
	out.Unit = to
	return out, nil
}

// ToMass converts a volume to grams using density in grams per millilitre.
// The second result reports whether the result is a mass. A mass is returned
// as is (true). With a zero density, or a unit that is not a volume or mass,
// a is returned unchanged and false: a density is never guessed (ADR-00005).
func ToMass(a Amount, density Rat) (Amount, bool) {
	switch a.Unit.Dimension {
	case DimMass:
		return a, true
	case DimVolume:
	default:
		return a, false
	}
	if density.Sign() <= 0 {
		return a, false
	}
	ml, err := Convert(a, MustUnit("ml"))
	if err != nil { // unreachable: volume converts to ml
		return a, false
	}
	out := ml.withQty(ml.Qty.Mul(density))
	out.Unit = MustUnit("g")
	return out, true
}

// Mode selects how an amount is displayed. The stored amount never changes.
type Mode int

const (
	// ModeAsWritten leaves the amount alone.
	ModeAsWritten Mode = iota
	// ModeMetric shows volume as mL or L and mass as g or kg.
	ModeMetric
	// ModeWeight shows volume as mass where a density exists, otherwise metric.
	ModeWeight
)

// Display returns a re-expressed for mode. density is grams per millilitre and
// is only consulted by ModeWeight; pass the zero Rat when unknown. Units that
// do not convert (count, other, none) are returned as written. The result is
// exact; apply Round for kitchen precision.
func Display(a Amount, mode Mode, density Rat) Amount {
	if mode == ModeAsWritten || a.Qty.IsAbsent() {
		return a
	}
	if mode == ModeWeight {
		if m, ok := ToMass(a, density); ok {
			a = m
		}
	}
	switch a.Unit.Dimension {
	case DimVolume:
		return metricStep(a, "ml", "l")
	case DimMass:
		return metricStep(a, "g", "kg")
	}
	return a
}

// metricStep converts a to the small metric unit, then to the large one when
// the amount reaches one of them.
func metricStep(a Amount, small, large string) Amount {
	out, err := Convert(a, MustUnit(small))
	if err != nil { // unreachable: callers pass a unit of the matching dimension
		return a
	}
	if larger, err := Convert(out, MustUnit(large)); err == nil && larger.Qty.Min().Cmp(Int(1)) >= 0 {
		return larger
	}
	return out
}
