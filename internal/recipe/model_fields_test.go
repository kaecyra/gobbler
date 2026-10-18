package recipe

import (
	"errors"
	"testing"
)

func TestValidateRecipeFields(t *testing.T) {
	two, four, neg := 2, 4, -1
	step := func(kind StageKind) Component {
		return Component{Steps: []Step{{ID: "a", Kind: kind, Title: "Mix"}}}
	}
	tests := []struct {
		name    string
		r       Recipe
		wantErr error
	}{
		{"all unset", rec(step("")), nil},
		{"unknown meal type", Recipe{MealType: "brunch", Components: []Component{step("")}}, ErrInvalidMealType},
		{"known meal type", Recipe{MealType: MealDessert, Components: []Component{step("")}}, nil},
		{"spice in range", Recipe{SpiceLevel: &two, Components: []Component{step("")}}, nil},
		{"spice too high", Recipe{SpiceLevel: &four, Components: []Component{step("")}}, ErrInvalidSpiceLevel},
		{"spice negative", Recipe{SpiceLevel: &neg, Components: []Component{step("")}}, ErrInvalidSpiceLevel},
		{"known stage kind", rec(step(StageBake)), nil},
		{"unknown stage kind", rec(step("fry")), ErrInvalidStageKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.r.Validate(); !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestClosedSetsMatchTheirValidators(t *testing.T) {
	for _, m := range MealTypes() {
		if !m.Valid() {
			t.Errorf("MealTypes() lists %q, which is not Valid", m)
		}
	}
	for _, k := range StageKinds() {
		if !k.Valid() {
			t.Errorf("StageKinds() lists %q, which is not Valid", k)
		}
	}
	if MealType("").Valid() || StageKind("").Valid() {
		t.Error("the empty value is unset, not a member of a closed set")
	}
}

func TestCuisineIsAClosedSetEndingInOther(t *testing.T) {
	all := Cuisines()
	if len(all) == 0 || all[len(all)-1] != CuisineOther {
		t.Fatalf("Cuisines() = %v, want a list ending in Other", all)
	}
	for _, c := range all {
		if !c.Valid() || c.Label() == "" {
			t.Errorf("cuisine %q: Valid=%v Label=%q", c, c.Valid(), c.Label())
		}
	}
	if Cuisine("").Valid() || Cuisine("asian").Valid() {
		t.Error("unset and unlisted cuisines must not be valid")
	}
	if got := CuisineMiddleEastern.Label(); got != "Middle Eastern" {
		t.Errorf("Label() = %q", got)
	}
	r := Recipe{Cuisine: "asian", Components: []Component{{Steps: []Step{{ID: "a", Title: "x"}}}}}
	if err := r.Validate(); !errors.Is(err, ErrInvalidCuisine) {
		t.Errorf("Validate() = %v, want ErrInvalidCuisine", err)
	}
	r.Cuisine = CuisineThai
	if err := r.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}
