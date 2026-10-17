package recipe

import (
	"errors"
	"fmt"
)

// Errors returned (wrapped) by Recipe.Validate for the optional fields
// ADR-00022 added to the recipe model.
var (
	// ErrInvalidMealType is returned for a meal type outside the closed set.
	ErrInvalidMealType = errors.New("invalid meal type")
	// ErrInvalidSpiceLevel is returned for a spice level outside 0 to 3.
	ErrInvalidSpiceLevel = errors.New("invalid spice level")
	// ErrInvalidStageKind is returned for a step stage kind outside the closed
	// set.
	ErrInvalidStageKind = errors.New("invalid stage kind")
)

// MealType groups a recipe in the library (ADR-00022). The empty value is
// unset, and is not a member of the set.
type MealType string

// The meal types, in the order a library lists them.
const (
	MealBreakfast MealType = "breakfast"
	MealMain      MealType = "main"
	MealSide      MealType = "side"
	MealDessert   MealType = "dessert"
	MealSnack     MealType = "snack"
	MealDrink     MealType = "drink"
)

// MealTypes lists the closed set of meal types.
func MealTypes() []MealType {
	return []MealType{MealBreakfast, MealMain, MealSide, MealDessert, MealSnack, MealDrink}
}

// Valid reports whether m is one of the defined meal types.
func (m MealType) Valid() bool {
	for _, k := range MealTypes() {
		if m == k {
			return true
		}
	}
	return false
}

// MaxSpiceLevel is the top of the spice scale; the bottom is 0.
const MaxSpiceLevel = 3

// StageKind says what a step is doing, for the time ribbon (ADR-00022). It sits
// beside Step.Passive, which keeps its meaning. The empty value is unset.
type StageKind string

// The stage kinds.
const (
	StagePrep     StageKind = "prep"
	StageRise     StageKind = "rise"
	StageRest     StageKind = "rest"
	StageSteep    StageKind = "steep"
	StageMarinate StageKind = "marinate"
	StageChill    StageKind = "chill"
	StageBake     StageKind = "bake"
	StageRoast    StageKind = "roast"
	StageSimmer   StageKind = "simmer"
	StageBraise   StageKind = "braise"
	StageCook     StageKind = "cook"
)

// StageKinds lists the closed set of stage kinds.
func StageKinds() []StageKind {
	return []StageKind{
		StagePrep, StageRise, StageRest, StageSteep, StageMarinate, StageChill,
		StageBake, StageRoast, StageSimmer, StageBraise, StageCook,
	}
}

// Valid reports whether k is one of the defined stage kinds.
func (k StageKind) Valid() bool {
	for _, s := range StageKinds() {
		if k == s {
			return true
		}
	}
	return false
}

// validateFields checks the optional fields ADR-00022 added; unset is valid.
func (r Recipe) validateFields() error {
	if r.MealType != "" && !r.MealType.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidMealType, r.MealType)
	}
	if s := r.SpiceLevel; s != nil && (*s < 0 || *s > MaxSpiceLevel) {
		return fmt.Errorf("%w: %d", ErrInvalidSpiceLevel, *s)
	}
	for _, ref := range r.LinearOrder() {
		if k := r.Components[ref.ComponentIndex].Steps[ref.Position-1].Kind; k != "" && !k.Valid() {
			return fmt.Errorf("%w: %s %q", ErrInvalidStageKind, ref, k)
		}
	}
	return nil
}
