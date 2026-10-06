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
// A mass, volume or count unit without a positive factor returns
// ErrInvalidUnit rather than converting to zero. Converting to the same unit always succeeds. An absent quantity is returned
// with its unit unchanged. The package size is carried over untouched.
func Convert(a Amount, to Unit) (Amount, error) {
	if a.Unit.Name == to.Name && a.Unit.Dimension == to.Dimension {
		return a, nil
	}
	if err := errors.Join(a.Unit.Validate(), to.Validate()); err != nil {
		return Amount{}, err
	}
	if !a.Unit.Converts() || !to.Converts() || a.Unit.Dimension != to.Dimension {
		return Amount{}, fmt.Errorf("%w: %q (%s) to %q (%s)", ErrNotConvertible,
			a.Unit.Name, a.Unit.Dimension, to.Name, to.Dimension)
	}
	if a.Qty.IsAbsent() {
		return a, nil
	}
	ratio, err := a.Unit.Factor.Div(to.Factor)
	if err != nil {
		return Amount{}, fmt.Errorf("convert %q to %q: %w", a.Unit.Name, to.Name, err)
	}
	q, err := a.Qty.Mul(ratio)
	if err != nil {
		return Amount{}, fmt.Errorf("convert %q to %q: %w", a.Unit.Name, to.Name, err)
	}
	out := a.withQty(q)
	out.Unit = to
	return out, nil
}

// ToMass converts a volume to grams using density in grams per millilitre.
// The bool reports whether the result is a mass. A mass is returned as is
// (true). With a zero density, or a unit that is not a volume or mass, a is
// returned unchanged and false: a density is never guessed (ADR-00005). The
// error is non-nil only for an invalid unit (ErrInvalidUnit).
func ToMass(a Amount, density Rat) (Amount, bool, error) {
	if err := a.Unit.Validate(); err != nil {
		return a, false, err
	}
	switch a.Unit.Dimension {
	case DimMass:
		return a, true, nil
	case DimVolume:
	default:
		return a, false, nil
	}
	if density.Sign() <= 0 {
		return a, false, nil
	}
	ml, err := Convert(a, MustUnit("ml"))
	if err != nil {
		return a, false, err
	}
	q, err := ml.Qty.Mul(density)
	if err != nil {
		return a, false, err
	}
	out := ml.withQty(q)
	out.Unit = MustUnit("g")
	return out, true, nil
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
// exact; apply Round for kitchen precision. The error is non-nil only for an
// invalid unit (ErrInvalidUnit).
func Display(a Amount, mode Mode, density Rat) (Amount, error) {
	if mode == ModeAsWritten || a.Qty.IsAbsent() {
		return a, nil
	}
	if mode == ModeWeight {
		m, ok, err := ToMass(a, density)
		if err != nil {
			return a, err
		}
		if ok {
			a = m
		}
	}
	if err := a.Unit.Validate(); err != nil {
		return a, err
	}
	switch a.Unit.Dimension {
	case DimVolume:
		return metricStep(a, "ml", "l")
	case DimMass:
		return metricStep(a, "g", "kg")
	}
	return a, nil
}

// metricStep converts a to the small metric unit, then to the large one when
// the amount reaches one of them.
func metricStep(a Amount, small, large string) (Amount, error) {
	out, err := Convert(a, MustUnit(small))
	if err != nil {
		return a, err
	}
	larger, err := Convert(out, MustUnit(large))
	if err != nil {
		return a, err
	}
	if larger.Qty.Min().Cmp(Int(1)) >= 0 {
		return larger, nil
	}
	return out, nil
}
