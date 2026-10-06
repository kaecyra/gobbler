package recipe

import (
	"errors"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
)

func goodLine() IngredientLine {
	return IngredientLine{
		Amount:     quantity.Amount{Qty: quantity.Exact(quantity.Int(2))},
		Ingredient: 7,
		Qualifiers: []Qualifier{{QualPreparation, "melted"}, {QualNote, "preferably homemade"}},
		Rule:       quantity.RuleLinear,
		Raw:        "2 tbsp butter, melted",
	}
}

func TestValidate(t *testing.T) {
	badRule, badKind, emptyText := goodLine(), goodLine(), goodLine()
	badRule.Rule = "sometimes"
	badKind.Qualifiers = []Qualifier{{"colour", "red"}}
	emptyText.Qualifiers = []Qualifier{{QualState, ""}}
	noRule := goodLine()
	noRule.Rule = ""
	badUnit := goodLine()
	badUnit.Amount.Unit = quantity.Unit{Name: "x", Dimension: quantity.DimMass}

	tests := []struct {
		name    string
		r       Recipe
		wantErr error
	}{
		{"single unnamed component", rec(Component{Lines: []IngredientLine{goodLine()}}), nil},
		{"no components", Recipe{}, ErrNoComponents},
		{"unknown rule", rec(Component{Lines: []IngredientLine{badRule}}), ErrInvalidLine},
		{"empty rule", rec(Component{Lines: []IngredientLine{noRule}}), ErrInvalidLine},
		{"bad unit", rec(Component{Lines: []IngredientLine{badUnit}}), ErrInvalidLine},
		{"unknown qualifier kind", rec(Component{Lines: []IngredientLine{badKind}}), ErrInvalidQualifier},
		{"empty qualifier text", rec(Component{Lines: []IngredientLine{emptyText}}), ErrInvalidQualifier},
		{"negative step duration", rec(Component{Steps: []Step{{ID: "a", Duration: -time.Minute}}}), ErrInvalidDuration},
		{"negative prep", Recipe{Components: []Component{{}}, Times: Times{Prep: -time.Minute}}, ErrInvalidDuration},
		{"negative cook", Recipe{Components: []Component{{}}, Times: Times{Cook: -time.Minute}}, ErrInvalidDuration},
		{"negative total", Recipe{Components: []Component{{}}, Times: Times{Total: -time.Minute}}, ErrInvalidDuration},
		{"negative special", Recipe{Components: []Component{{}}, Times: Times{Special: []SpecialTime{{LabelRise, -time.Minute}}}}, ErrInvalidDuration},
		{"cycle surfaces", rec(Component{Steps: []Step{st("a", "a")}}), ErrCycle},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.r.Validate(); !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestQualifierKindValid(t *testing.T) {
	for _, k := range []QualifierKind{QualPreparation, QualState, QualForm, QualNote} {
		if !k.Valid() {
			t.Errorf("%q should be valid", k)
		}
	}
	if QualifierKind("").Valid() {
		t.Error("empty kind should be invalid")
	}
}
