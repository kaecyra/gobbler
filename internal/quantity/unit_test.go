package quantity

import "testing"

func TestBuiltinUnitTable(t *testing.T) {
	tests := []struct {
		name   string
		sys    System
		dim    Dimension
		factor string // exact, in base units
	}{
		{"g", SystemMetric, DimMass, "1"},
		{"kg", SystemMetric, DimMass, "1000"},
		{"oz", SystemUS, DimMass, "28.349523125"},
		{"lb", SystemUS, DimMass, "453.59237"},
		{"ml", SystemMetric, DimVolume, "1"},
		{"l", SystemMetric, DimVolume, "1000"},
		{"tsp", SystemUS, DimVolume, "4.92892159375"},
		{"tbsp", SystemUS, DimVolume, "14.78676478125"},
		{"cup", SystemUS, DimVolume, "236.5882365"},
		{"each", SystemNone, DimCount, "1"},
		{"dozen", SystemNone, DimCount, "12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, ok := LookupUnit(tt.name)
			if !ok {
				t.Fatal("unit missing")
			}
			if u.System != tt.sys || u.Dimension != tt.dim || !u.Factor.Equal(ratStr(tt.factor)) {
				t.Errorf("got %+v", u)
			}
			if !u.Converts() || u.IsZero() {
				t.Error("Converts/IsZero wrong")
			}
		})
	}
}

func TestUsCustomaryVolumesAreConsistent(t *testing.T) {
	tsp := MustUnit("tsp").Factor
	checks := []struct {
		unit string
		tsp  int64
	}{{"tbsp", 3}, {"fl oz", 6}, {"cup", 48}, {"pint", 96}, {"quart", 192}, {"gallon", 768}}
	for _, c := range checks {
		if got := tsp.Mul(Int(c.tsp)); !got.Equal(MustUnit(c.unit).Factor) {
			t.Errorf("%d tsp = %s mL, but %s is %s mL", c.tsp, got.Decimal(10), c.unit, MustUnit(c.unit).Factor.Decimal(10))
		}
	}
}

func TestOtherUnitsDoNotConvertAndLookupFails(t *testing.T) {
	pinch := OtherUnit("pinch")
	if pinch.Converts() || pinch.Dimension != DimOther || pinch.System != SystemNone {
		t.Errorf("pinch = %+v", pinch)
	}
	if _, ok := LookupUnit("tablespoon"); ok {
		t.Error("aliases are not this package's concern")
	}
	if !(Unit{}).IsZero() {
		t.Error("zero unit")
	}
}

func TestMustUnitPanicsOnUnknown(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("want panic")
		}
	}()
	MustUnit("furlong")
}

func TestUnitsReturnsACopy(t *testing.T) {
	us := Units()
	us[0].Name = "mutated"
	if MustUnit("g").Name != "g" {
		t.Error("table was mutated through the copy")
	}
}
