package recipeview

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Query parameters of the recipe view. The view is a pure function of the
// stored recipe and these, so every scaled or converted state is a shareable
// URL and nothing about it is saved (ADR-00005: conversion is a view).
const (
	paramScale    = "scale"
	paramServings = "servings"
	paramUnits    = "units"
)

// Values of the units parameter. The empty value is "as written".
const (
	unitsWritten = "written"
	unitsMetric  = "metric"
	unitsWeight  = "weight"
)

// errNoServingsBase marks a servings target on a recipe whose servings are a
// range or absent, so there is no single count to scale from. The page tells
// the user to scale by a multiplier instead.
var errNoServingsBase = errors.New("recipe has no single serving count to scale from")

// options is the parsed view state. scale and servings keep the raw text so
// the controls can rebuild the same URL; at most one is set.
type options struct {
	mult     quantity.Rat
	mode     quantity.Mode
	units    string // normalised: "", unitsMetric or unitsWeight
	scale    string
	servings string
}

// scaled reports whether the multiplier differs from 1.
func (o options) scaled() bool { return !o.mult.Equal(quantity.Int(1)) }

// parseOptions validates the query against the recipe. Every failure wraps
// layout.ErrInvalid: the request asked for something that cannot be shown.
func parseOptions(q url.Values, r recipe.Recipe) (options, error) {
	o := options{mult: quantity.Int(1)}

	switch u := q.Get(paramUnits); u {
	case "", unitsWritten:
	case unitsMetric:
		o.mode, o.units = quantity.ModeMetric, unitsMetric
	case unitsWeight:
		o.mode, o.units = quantity.ModeWeight, unitsWeight
	default:
		return options{}, fmt.Errorf("%w: units %q", layout.ErrInvalid, u)
	}

	scale, servings := q.Get(paramScale), q.Get(paramServings)
	switch {
	case scale != "" && servings != "":
		return options{}, fmt.Errorf("%w: both %s and %s given", layout.ErrInvalid, paramScale, paramServings)
	case scale != "":
		m, err := quantity.ParseRat(scale)
		if err != nil {
			return options{}, fmt.Errorf("%w: scale %q: %w", layout.ErrInvalid, scale, err)
		}
		if m.Sign() <= 0 {
			return options{}, fmt.Errorf("%w: scale %q is not positive", layout.ErrInvalid, scale)
		}
		o.mult, o.scale = m, scale
	case servings != "":
		target, err := quantity.ParseRat(servings)
		if err != nil {
			return options{}, fmt.Errorf("%w: servings %q: %w", layout.ErrInvalid, servings, err)
		}
		if r.Servings.Kind() != quantity.KindExact {
			return options{}, fmt.Errorf("%w: %w", layout.ErrInvalid, errNoServingsBase)
		}
		m, err := quantity.ServingsFactor(r.Servings.Min(), target)
		if err != nil {
			return options{}, fmt.Errorf("%w: %w", layout.ErrInvalid, err)
		}
		o.mult, o.servings = m, servings
	}
	return o, nil
}
