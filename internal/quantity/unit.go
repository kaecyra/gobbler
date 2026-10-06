package quantity

import (
	"errors"
	"fmt"
	"math/big"
)

// ErrInvalidUnit is returned for a mass, volume or count unit whose factor to
// its base unit is zero or negative.
var ErrInvalidUnit = errors.New("invalid unit")

// System is the measurement system a unit belongs to.
type System string

// Measurement systems.
const (
	SystemUS     System = "us"
	SystemMetric System = "metric"
	SystemNone   System = "none"
)

// Dimension says what a unit measures and whether it converts.
type Dimension string

// Unit dimensions. Mass, volume and count convert within themselves; other
// units ("pinch", "clove", "can") never convert.
const (
	DimMass   Dimension = "mass"
	DimVolume Dimension = "volume"
	DimCount  Dimension = "count"
	DimOther  Dimension = "other"
)

// Unit is a canonical unit. Within mass, volume and count, Factor is the exact
// number of base units (gram, millilitre, each) in one of this unit. Factor is
// zero for DimOther. The zero Unit means "no unit" ("2 eggs").
type Unit struct {
	Name      string
	System    System
	Dimension Dimension
	Factor    Rat
}

// IsZero reports whether u is the absent unit.
func (u Unit) IsZero() bool { return u.Name == "" }

// hasBaseDimension reports whether the dimension is one that converts.
func (u Unit) hasBaseDimension() bool {
	return u.Dimension == DimMass || u.Dimension == DimVolume || u.Dimension == DimCount
}

// Converts reports whether the unit can take part in conversion: it measures
// mass, volume or count and has a positive factor to its base unit.
func (u Unit) Converts() bool {
	return u.hasBaseDimension() && u.Factor.Sign() > 0
}

// Validate returns ErrInvalidUnit when the unit claims a convertible
// dimension but has no positive factor, as a unit built from a bad stored row
// would. Units in DimOther and the zero Unit are valid.
func (u Unit) Validate() error {
	if u.hasBaseDimension() && u.Factor.Sign() <= 0 {
		return fmt.Errorf("%w: %q (%s) has factor %s", ErrInvalidUnit, u.Name, u.Dimension, u.Factor)
	}
	return nil
}

// OtherUnit returns a non-converting unit such as "pinch" or "clove".
func OtherUnit(name string) Unit {
	return Unit{Name: name, System: SystemNone, Dimension: DimOther}
}

func ratStr(s string) Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic("quantity: bad built-in constant " + s) // programmer error, unreachable
	}
	return Rat{v: r}
}

func unit(name string, sys System, dim Dimension, factor string) Unit {
	return Unit{Name: name, System: sys, Dimension: dim, Factor: ratStr(factor)}
}

// builtinUnits is read-only after initialisation. Factors are exact: the US
// customary volume units are defined from the international inch, so one US
// teaspoon is exactly 4.92892159375 mL.
var builtinUnits = []Unit{
	unit("g", SystemMetric, DimMass, "1"),
	unit("kg", SystemMetric, DimMass, "1000"),
	unit("oz", SystemUS, DimMass, "28.349523125"),
	unit("lb", SystemUS, DimMass, "453.59237"),

	unit("ml", SystemMetric, DimVolume, "1"),
	unit("l", SystemMetric, DimVolume, "1000"),
	unit("tsp", SystemUS, DimVolume, "4.92892159375"),
	unit("tbsp", SystemUS, DimVolume, "14.78676478125"),
	unit("fl oz", SystemUS, DimVolume, "29.5735295625"),
	unit("cup", SystemUS, DimVolume, "236.5882365"),
	unit("pint", SystemUS, DimVolume, "473.176473"),
	unit("quart", SystemUS, DimVolume, "946.352946"),
	unit("gallon", SystemUS, DimVolume, "3785.411784"),

	unit("each", SystemNone, DimCount, "1"),
	unit("dozen", SystemNone, DimCount, "12"),
}

// LookupUnit returns the built-in unit with the given canonical name.
// Aliases ("T", "tablespoon") are the catalog's concern, not this package's.
func LookupUnit(name string) (Unit, bool) {
	for _, u := range builtinUnits {
		if u.Name == name {
			return u, true
		}
	}
	return Unit{}, false
}

// MustUnit is LookupUnit for names known at compile time; it panics on an
// unknown name, which is a programmer error.
func MustUnit(name string) Unit {
	u, ok := LookupUnit(name)
	if !ok {
		panic("quantity: unknown built-in unit " + name)
	}
	return u
}

// Units returns a copy of the built-in unit table.
func Units() []Unit {
	return append([]Unit(nil), builtinUnits...)
}
