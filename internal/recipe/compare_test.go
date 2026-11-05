package recipe

import (
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

func ln(key string, ing IngredientID, qty int64) IngredientLine {
	return IngredientLine{Key: key, Ingredient: ing, Rule: quantity.RuleLinear, Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(qty))}}
}

func stp(id, text string, deps ...StepID) Step {
	return Step{ID: StepID(id), Text: text, DependsOn: deps}
}

func one(lines []IngredientLine, steps []Step) Recipe {
	return Recipe{Name: "Bread", Components: []Component{{Lines: lines, Steps: steps}}}
}

const (
	k1 = "lin-aaaaaaaa"
	k2 = "lin-bbbbbbbb"
	k3 = "lin-cccccccc"
	s1 = "stp-aaaaaaaa"
	s2 = "stp-bbbbbbbb"
	s3 = "stp-cccccccc"
)

func TestCompareSameRecipeIsEmpty(t *testing.T) {
	r := one([]IngredientLine{ln(k1, 1, 2)}, []Step{stp(s1, "Mix.")})
	if d := Compare(r, r); !d.Empty() {
		t.Fatalf("diff of a recipe with itself = %+v", d)
	}
}

// A line inserted at the top must not make every line below it look changed:
// that is what pairing by key, and not by position, is for (ADR-00035).
func TestCompareInsertedLineLeavesTheRestUnchanged(t *testing.T) {
	old := one([]IngredientLine{ln(k1, 1, 2), ln(k2, 2, 3)}, nil)
	next := one([]IngredientLine{ln("", 9, 1), ln(k1, 1, 2), ln(k2, 2, 3)}, nil)
	d := Compare(old, next)
	if len(d.Lines) != 1 || d.Lines[0].Kind != Added || d.Lines[0].New.Ingredient != 9 {
		t.Fatalf("lines = %+v, want one added line", d.Lines)
	}
}

func TestCompareEditedRemovedAndAdded(t *testing.T) {
	old := one([]IngredientLine{ln(k1, 1, 2), ln(k2, 2, 3)}, []Step{stp(s1, "Mix."), stp(s2, "Bake.", StepID(s1))})
	next := one([]IngredientLine{ln(k1, 1, 4), ln("", 5, 1)}, []Step{stp(s1, "Mix well."), stp("new-1", "Cool.")})
	d := Compare(old, next)
	kinds := map[ChangeKind]int{}
	for _, c := range d.Lines {
		kinds[c.Kind]++
	}
	if kinds[Changed] != 1 || kinds[Removed] != 1 || kinds[Added] != 1 {
		t.Fatalf("line kinds = %v, want one of each", kinds)
	}
	c, ok := d.LineByOldKey(k1)
	if !ok || !c.Edited || c.Moved {
		t.Fatalf("line %s change = %+v", k1, c)
	}
	sc, ok := d.StepByOldID(s1)
	if !ok || sc.Old.Text != "Mix." || sc.New.Text != "Mix well." {
		t.Fatalf("step change = %+v", sc)
	}
	if sc, ok := d.StepByOldID(s2); !ok || sc.Kind != Removed {
		t.Fatalf("step %s change = %+v, want removed", s2, sc)
	}
}

// A new step may take the key of a removed one only by being the same element;
// a step with a different key is a different step even at the same position.
func TestCompareDoesNotPairByPositionWhenKeysExist(t *testing.T) {
	old := one(nil, []Step{stp(s1, "Mix.")})
	next := one(nil, []Step{stp(s2, "Mix.")})
	d := Compare(old, next)
	if len(d.Steps) != 2 {
		t.Fatalf("steps = %+v, want a removal and an addition", d.Steps)
	}
}

func TestCompareAgainstARevisionWithoutKeysPairsByPosition(t *testing.T) {
	old := one([]IngredientLine{ln("", 1, 2), ln("", 2, 3)}, []Step{stp("a", "Mix."), stp("b", "Bake.")})
	next := one([]IngredientLine{ln("", 1, 2), ln("", 2, 4)}, []Step{stp("a", "Mix."), stp("b", "Bake hot.")})
	d := Compare(old, next)
	if !d.ByPosition {
		t.Fatal("ByPosition = false for an old version with no keys")
	}
	if len(d.Lines) != 1 || d.Lines[0].Kind != Changed || len(d.Steps) != 1 || d.Steps[0].Kind != Changed {
		t.Fatalf("diff = %+v, want the second line and the second step changed", d)
	}
}

func TestCompareReorderIsAMove(t *testing.T) {
	old := one(nil, []Step{stp(s1, "A."), stp(s2, "B."), stp(s3, "C.")})
	next := one(nil, []Step{stp(s3, "C."), stp(s1, "A."), stp(s2, "B.")})
	d := Compare(old, next)
	if len(d.Steps) != 1 || !d.Steps[0].Moved || d.Steps[0].Edited || d.Steps[0].Old.ID != StepID(s3) {
		t.Fatalf("steps = %+v, want only %s moved", d.Steps, s3)
	}
}

func TestCompareFieldsAndParts(t *testing.T) {
	old := one(nil, nil)
	old.Components[0].Name = "Dough"
	next := one(nil, nil)
	next.Name = "Better bread"
	next.Components[0].Name = "Loaf"
	d := Compare(old, next)
	if len(d.Fields) != 1 || d.Fields[0].Field != FieldName || d.Fields[0].Old != "Bread" || d.Fields[0].New != "Better bread" {
		t.Fatalf("fields = %+v", d.Fields)
	}
	if len(d.Components) != 1 || d.Components[0].Kind != Changed || d.Components[0].NewName != "Loaf" {
		t.Fatalf("components = %+v", d.Components)
	}
}

func TestCompareEquipment(t *testing.T) {
	old := Recipe{Name: "x", Components: []Component{{}}, Equipment: []EquipmentLink{{Equipment: 1}, {Equipment: 2}}}
	next := Recipe{Name: "x", Components: []Component{{}}, Equipment: []EquipmentLink{{Equipment: 2}, {Equipment: 3}}}
	d := Compare(old, next)
	if len(d.Equipment) != 2 {
		t.Fatalf("equipment = %+v, want 1 removed and 3 added", d.Equipment)
	}
}
