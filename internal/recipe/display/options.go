// Package display holds the pure logic that turns a stored recipe into what a
// page shows: scale and unit-mode options, formatted ingredient lines, step
// numbers, durations and meanwhile guidance. It is shared by the recipe view
// and cook mode, which may not import each other (ADR-00012). It does no I/O of
// its own beyond the catalog lookups its callers hand it, and it never changes
// the stored recipe (ADR-00005: scaling and conversion are views).
package display

import (
	"errors"
	"fmt"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// Query parameter names of every page that shows a scaled or converted recipe,
// so a state is the same shareable URL vocabulary everywhere.
const (
	ParamScale    = "scale"
	ParamServings = "servings"
	ParamUnits    = "units"
)

// Values of the units parameter. The empty value is "as written".
const (
	UnitsWritten = "written"
	UnitsMetric  = "metric"
	UnitsWeight  = "weight"
)

// ErrInvalid marks a scale, servings or units value that cannot be shown. The
// web layer maps it to 400 (layout's statusTable); its text is what the error
// page shows, so it reads as a request problem.
var ErrInvalid = errors.New("invalid request")

// ErrNoServingsBase marks a servings target on a recipe whose servings are a
// range or absent, so there is no single count to scale from. It also matches
// ErrInvalid.
var ErrNoServingsBase = errors.New("recipe has no single serving count to scale from")

// Options is the parsed view state. Scale and Servings keep the raw text so
// controls can rebuild the same URL; at most one is set.
type Options struct {
	Mult     quantity.Rat
	Mode     quantity.Mode
	Units    string // normalised: "", UnitsMetric or UnitsWeight
	Scale    string
	Servings string
}

// Default returns the options of an unscaled, as-written view.
func Default() Options { return Options{Mult: quantity.Int(1)} }

// Scaled reports whether the multiplier differs from 1.
func (o Options) Scaled() bool { return !o.Mult.Equal(quantity.Int(1)) }

// ParseOptions validates the raw scale, servings and units values against the
// recipe. Empty means not given. Every failure wraps ErrInvalid.
func ParseOptions(scale, servings, units string, r recipe.Recipe) (Options, error) {
	o := Default()

	switch units {
	case "", UnitsWritten:
	case UnitsMetric:
		o.Mode, o.Units = quantity.ModeMetric, UnitsMetric
	case UnitsWeight:
		o.Mode, o.Units = quantity.ModeWeight, UnitsWeight
	default:
		return Options{}, fmt.Errorf("%w: units %q", ErrInvalid, units)
	}

	switch {
	case scale != "" && servings != "":
		return Options{}, fmt.Errorf("%w: both %s and %s given", ErrInvalid, ParamScale, ParamServings)
	case scale != "":
		m, err := quantity.ParseRat(scale)
		if err != nil {
			return Options{}, fmt.Errorf("%w: scale %q: %w", ErrInvalid, scale, err)
		}
		if m.Sign() <= 0 {
			return Options{}, fmt.Errorf("%w: scale %q is not positive", ErrInvalid, scale)
		}
		o.Mult, o.Scale = m, scale
	case servings != "":
		target, err := quantity.ParseRat(servings)
		if err != nil {
			return Options{}, fmt.Errorf("%w: servings %q: %w", ErrInvalid, servings, err)
		}
		if r.Servings.Kind() != quantity.KindExact {
			return Options{}, fmt.Errorf("%w: %w", ErrInvalid, ErrNoServingsBase)
		}
		m, err := quantity.ServingsFactor(r.Servings.Min(), target)
		if err != nil {
			return Options{}, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		o.Mult, o.Servings = m, servings
	}
	return o, nil
}

// ScaleQty scales a quantity linearly and renders it. Servings and yield have
// no unit, so none of the unit modes apply.
func ScaleQty(q quantity.Quantity, o Options) (string, error) {
	a, err := quantity.Scale(quantity.Amount{Qty: q}, quantity.RuleLinear, o.Mult)
	if err != nil {
		return "", err
	}
	if !o.Scaled() {
		return a.Qty.String(), nil
	}
	r, err := quantity.Round(a)
	if err != nil {
		return "", err
	}
	return r.Rounded.Qty.String(), nil
}
