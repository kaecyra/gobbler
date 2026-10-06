package quantity

import (
	"errors"
	"testing"
)

func TestQuantityKindsAreDistinguishable(t *testing.T) {
	exact := Exact(Int(2))
	r, err := NewRange(Int(2), Int(3))
	if err != nil {
		t.Fatal(err)
	}
	absent := Absent()

	if !absent.IsAbsent() || exact.IsAbsent() || r.IsAbsent() {
		t.Error("IsAbsent wrong")
	}
	if !r.IsRange() || exact.IsRange() || absent.IsRange() {
		t.Error("IsRange wrong")
	}
	if (Quantity{}).Kind() != KindAbsent {
		t.Error("zero value must be absent")
	}
	if exact.Equal(r) || exact.Equal(absent) || r.Equal(absent) || !exact.Equal(Exact(Int(2))) {
		t.Error("Equal wrong")
	}
	// A degenerate range is still a range, not an exact value.
	same, _ := NewRange(Int(2), Int(2))
	if same.Equal(exact) {
		t.Error("range 2-2 must differ from exact 2")
	}
	if r.Min().String() != "2" || r.Max().String() != "3" {
		t.Errorf("bounds = %s..%s", r.Min(), r.Max())
	}
}

func TestNewRangeRejectsReversedBounds(t *testing.T) {
	if _, err := NewRange(Int(3), Int(2)); !errors.Is(err, ErrInvalidRange) {
		t.Errorf("err = %v", err)
	}
}

func TestQuantityMulDiv(t *testing.T) {
	r, _ := NewRange(rat(t, 1, 2), Int(1))
	if got := r.Mul(Int(2)); got.String() != "1-2" {
		t.Errorf("range*2 = %s", got)
	}
	if got := Absent().Mul(Int(2)); !got.IsAbsent() {
		t.Error("absent*2 must stay absent")
	}
	got, err := Exact(Int(3)).Div(Int(2))
	if err != nil || got.String() != "1 1/2" {
		t.Errorf("3/2 = %s, %v", got, err)
	}
	if got, err := Absent().Div(Int(2)); err != nil || !got.IsAbsent() {
		t.Errorf("absent/2 = %v, %v", got, err)
	}
	if _, err := Exact(Int(3)).Div(Rat{}); !errors.Is(err, ErrDivideByZero) {
		t.Errorf("div by zero err = %v", err)
	}
	if got, _ := r.Div(Int(2)); got.String() != "1/4-1/2" {
		t.Errorf("range/2 = %s", got)
	}
}

func TestAmountString(t *testing.T) {
	can := amt(t, 14, 1, "oz")
	tests := []struct {
		name string
		in   Amount
		want string
	}{
		{"fraction", amt(t, 3, 2, "cup"), "1 1/2 cup"},
		{"metric decimal", amt(t, 5, 2, "g"), "2.5 g"},
		{"range", rng(t, 2, 3, "tbsp"), "2-3 tbsp"},
		{"unitless", Amount{Qty: Exact(Int(2))}, "2"},
		{"absent with unit", Amount{Unit: OtherUnit("pinch")}, "pinch"},
		{"package", Amount{Qty: Exact(Int(1)), Unit: OtherUnit("can"), Package: &can}, "1 (14 oz) can"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPackageSizeIsRepresentable(t *testing.T) {
	size := amt(t, 14, 1, "oz")
	a := Amount{Qty: Exact(Int(2)), Unit: OtherUnit("can"), Package: &size}
	if a.Package == nil || a.Package.Unit.Name != "oz" || !a.Package.Qty.Min().Equal(Int(14)) {
		t.Errorf("package = %+v", a.Package)
	}
}
