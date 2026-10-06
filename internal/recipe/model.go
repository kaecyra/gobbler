package recipe

import (
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// Sentinel errors returned (wrapped) by Recipe.Validate.
var (
	// ErrNoComponents is returned for a recipe with no components. A recipe
	// with no natural parts has one unnamed component.
	ErrNoComponents = errors.New("recipe has no components")
	// ErrInvalidQualifier is returned for a qualifier of an unknown kind or
	// with no text.
	ErrInvalidQualifier = errors.New("invalid qualifier")
	// ErrInvalidLine is returned for an ingredient line that breaks a
	// structural rule.
	ErrInvalidLine = errors.New("invalid ingredient line")
	// ErrInvalidDuration is returned for a negative step duration or time.
	ErrInvalidDuration = errors.New("negative duration")
)

// IngredientID references a canonical ingredient (ADR-00008). The catalog owns
// the entity; the recipe holds only the reference. Zero means unresolved.
type IngredientID int64

// EquipmentID references a catalog piece of equipment.
type EquipmentID int64

// StepID identifies a step within one recipe. It is the target of step
// dependencies, so it must be non-empty and unique across all components.
type StepID string

// QualifierKind types a qualifier on an ingredient line.
type QualifierKind string

// Qualifier kinds (ADR-00004).
const (
	// QualPreparation is what is done to it: "melted", "finely chopped".
	QualPreparation QualifierKind = "preparation"
	// QualState is its condition: "at room temperature", "cold".
	QualState QualifierKind = "state"
	// QualForm is its form: "powdered", "ground".
	QualForm QualifierKind = "form"
	// QualNote is anything else: "preferably homemade".
	QualNote QualifierKind = "note"
)

// Valid reports whether k is one of the four defined kinds.
func (k QualifierKind) Valid() bool {
	switch k {
	case QualPreparation, QualState, QualForm, QualNote:
		return true
	}
	return false
}

// Qualifier is one typed phrase attached to an ingredient line.
type Qualifier struct {
	Kind QualifierKind
	Text string
}

// IngredientLine is one structured line of a component's ingredient list.
type IngredientLine struct {
	// Amount holds the quantity (exact, range or absent), the unit (zero for
	// count-only items such as "2 eggs") and any package size.
	Amount quantity.Amount
	// Ingredient references the canonical ingredient.
	Ingredient IngredientID
	// Qualifiers are ordered as written.
	Qualifiers []Qualifier
	Optional   bool
	// Rule says how the line scales; the empty rule is invalid here, use
	// quantity.ParseRule to turn stored text into one.
	Rule quantity.Rule
	// Raw is the original line, kept for display and re-parsing.
	Raw string
}

// Yield is what the recipe makes: a quantity and free-text unit ("1 loaf").
type Yield struct {
	Qty  quantity.Quantity
	Unit string
}

// EquipmentLink ties a piece of equipment to a recipe, optionally with a
// quantity ("2 baking sheets") and optionally to the steps that use it.
type EquipmentLink struct {
	Equipment EquipmentID
	Qty       quantity.Quantity
	Steps     []StepID
}

// Step is one instruction. Duration zero means unknown. Passive steps are
// those that need no attention while they run (rising, baking, marinating).
type Step struct {
	ID       StepID
	Text     string
	Duration time.Duration
	Passive  bool
	// DependsOn lists steps, in any component of the same recipe, that must
	// finish first.
	DependsOn []StepID
}

// Component is an ordered part of a recipe ("Dough", "Sauce"). Its name is
// empty for the single unnamed component of a recipe without parts.
type Component struct {
	Name  string
	Lines []IngredientLine
	Steps []Step
}

// Recipe is the structured recipe document. CaloriesPerServing is entered or
// scraped and never computed (ADR-00011); nil means not stated. Rating is nil
// when unrated.
type Recipe struct {
	Name               string
	Description        string
	SourceURL          string
	Author             string
	Yield              Yield
	Servings           quantity.Quantity
	CaloriesPerServing *int
	Tags               []string
	Rating             *int
	Times              Times
	Components         []Component
	Equipment          []EquipmentLink
}

// Validate checks the structural rules of the model: at least one component,
// well-formed lines and qualifiers, non-negative durations, and a valid step graph (see
// ValidateSteps). It does not judge content such as an empty name.
func (r Recipe) Validate() error {
	if len(r.Components) == 0 {
		return ErrNoComponents
	}
	for ci, c := range r.Components {
		for li, l := range c.Lines {
			if err := l.validate(); err != nil {
				return fmt.Errorf("component %d line %d: %w", ci+1, li+1, err)
			}
		}
	}
	if err := r.validateDurations(); err != nil {
		return err
	}
	return r.ValidateSteps()
}

func (r Recipe) validateDurations() error {
	t := r.Times
	for _, f := range []struct {
		name string
		d    time.Duration
	}{{"prep time", t.Prep}, {"cook time", t.Cook}, {"total time", t.Total}} {
		if f.d < 0 {
			return fmt.Errorf("%w: %s %s", ErrInvalidDuration, f.name, f.d)
		}
	}
	for i, s := range t.Special {
		if s.Duration < 0 {
			return fmt.Errorf("%w: special time %d (%q) %s", ErrInvalidDuration, i+1, s.Label, s.Duration)
		}
	}
	for _, ref := range r.LinearOrder() {
		if d := r.Components[ref.ComponentIndex].Steps[ref.Position-1].Duration; d < 0 {
			return fmt.Errorf("%w: %s %s", ErrInvalidDuration, ref, d)
		}
	}
	return nil
}

func (l IngredientLine) validate() error {
	if _, err := quantity.ParseRule(string(l.Rule)); err != nil || l.Rule == "" {
		return fmt.Errorf("%w: scaling rule %q", ErrInvalidLine, l.Rule)
	}
	if err := l.Amount.Unit.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidLine, err)
	}
	for _, q := range l.Qualifiers {
		if !q.Kind.Valid() || q.Text == "" {
			return fmt.Errorf("%w: kind %q text %q", ErrInvalidQualifier, q.Kind, q.Text)
		}
	}
	return nil
}
