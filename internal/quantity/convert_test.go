package quantity

import (
	"errors"
	"testing"
)

func TestConvertWithinDimensionIsExact(t *testing.T) {
	tests := []struct {
		name string
		in   Amount
		to   string
		want string
	}{
		{"tsp to tbsp", amt(t, 3, 1, "tsp"), "tbsp", "1 tbsp"},
		{"48 tsp to cup", amt(t, 48, 1, "tsp"), "cup", "1 cup"},
		{"cup to tbsp", amt(t, 1, 3, "cup"), "tbsp", "5 1/3 tbsp"},
		{"lb to oz", amt(t, 1, 1, "lb"), "oz", "16 oz"},
		{"kg to g", amt(t, 3, 2, "kg"), "g", "1500 g"},
		{"ml to l", amt(t, 250, 1, "ml"), "l", "0.25 l"},
		{"cup to ml", amt(t, 1, 1, "cup"), "ml", "236.59 ml"},
		{"range", rng(t, 1, 2, "tbsp"), "tsp", "3-6 tsp"},
		{"dozen to each", amt(t, 1, 2, "dozen"), "each", "6 each"},
		{"same unit", amt(t, 1, 2, "cup"), "cup", "1/2 cup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Convert(tt.in, MustUnit(tt.to))
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConvertRoundTripIsLossless(t *testing.T) {
	in := amt(t, 7, 3, "cup")
	ml, err := Convert(in, MustUnit("ml"))
	if err != nil {
		t.Fatal(err)
	}
	back, err := Convert(ml, MustUnit("cup"))
	if err != nil {
		t.Fatal(err)
	}
	if !back.Qty.Equal(in.Qty) {
		t.Errorf("round trip = %s, want %s", back.Qty, in.Qty)
	}
}

func TestConvertRefusesWhatCannotConvert(t *testing.T) {
	tests := []struct {
		name string
		in   Amount
		to   Unit
	}{
		{"other to other", Amount{Qty: Exact(Int(1)), Unit: OtherUnit("clove")}, OtherUnit("pinch")},
		{"other to volume", Amount{Qty: Exact(Int(1)), Unit: OtherUnit("clove")}, MustUnit("cup")},
		{"volume to mass needs density", amt(t, 1, 1, "cup"), MustUnit("g")},
		{"mass to count", amt(t, 1, 1, "g"), MustUnit("each")},
		{"unitless to volume", Amount{Qty: Exact(Int(1))}, MustUnit("cup")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Convert(tt.in, tt.to); !errors.Is(err, ErrNotConvertible) {
				t.Errorf("err = %v, want ErrNotConvertible", err)
			}
		})
	}
}

func TestConvertSameOtherUnitSucceeds(t *testing.T) {
	a := Amount{Qty: Exact(Int(2)), Unit: OtherUnit("clove")}
	got, err := Convert(a, OtherUnit("clove"))
	if err != nil || got.String() != "2 clove" {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestConvertAbsentAndPackage(t *testing.T) {
	a := Amount{Unit: MustUnit("cup")}
	got, err := Convert(a, MustUnit("ml"))
	if err != nil || !got.Qty.IsAbsent() {
		t.Errorf("absent convert = %v, %v", got, err)
	}
	b := canOf(2)
	b.Unit = MustUnit("cup")
	got, _ = Convert(b, MustUnit("tbsp"))
	if !got.HasPackage || got.Package.Unit.Name != "oz" || !got.Package.Qty.Min().Equal(Int(14)) {
		t.Error("package should be carried over")
	}
}

func TestToMass(t *testing.T) {
	flour := rat(t, 1, 2) // 0.5 g/ml
	t.Run("volume with density", func(t *testing.T) {
		got, ok, err := ToMass(amt(t, 1, 1, "cup"), flour)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || got.Unit.Name != "g" {
			t.Fatalf("got %v, %v", got, ok)
		}
		if want := MustUnit("cup").Factor.Mul(flour); !got.Qty.Min().Equal(want) {
			t.Errorf("qty = %s, want %s", got.Qty, want)
		}
	})
	t.Run("no density keeps volume", func(t *testing.T) {
		in := amt(t, 1, 1, "cup")
		got, ok, _ := ToMass(in, Rat{})
		if ok || got.String() != in.String() || got.Unit.Dimension != DimVolume {
			t.Errorf("got %v, %v", got, ok)
		}
	})
	t.Run("negative density treated as unknown", func(t *testing.T) {
		if _, ok, _ := ToMass(amt(t, 1, 1, "cup"), Int(-1)); ok {
			t.Error("want not converted")
		}
	})
	t.Run("mass passes through", func(t *testing.T) {
		got, ok, _ := ToMass(amt(t, 3, 1, "oz"), Rat{})
		if !ok || got.Unit.Name != "oz" {
			t.Errorf("got %v, %v", got, ok)
		}
	})
	t.Run("count and other stay", func(t *testing.T) {
		if _, ok, _ := ToMass(amt(t, 3, 1, "each"), flour); ok {
			t.Error("count converted")
		}
		if _, ok, _ := ToMass(Amount{Qty: Exact(Int(1)), Unit: OtherUnit("pinch")}, flour); ok {
			t.Error("other converted")
		}
	})
}

func TestDisplayModes(t *testing.T) {
	flour := rat(t, 1, 2)
	tests := []struct {
		name    string
		in      Amount
		mode    Mode
		density Rat
		want    string
	}{
		{"as written", amt(t, 1, 1, "cup"), ModeAsWritten, flour, "1 cup"},
		{"metric volume small", amt(t, 1, 1, "tsp"), ModeMetric, Rat{}, "4.93 ml"},
		{"metric volume to litres", amt(t, 5, 1, "cup"), ModeMetric, Rat{}, "1.18 l"},
		{"metric mass small", amt(t, 1, 1, "oz"), ModeMetric, Rat{}, "28.35 g"},
		{"metric mass to kg", amt(t, 5, 1, "lb"), ModeMetric, Rat{}, "2.27 kg"},
		{"metric ignores density", amt(t, 1, 1, "cup"), ModeMetric, flour, "236.59 ml"},
		{"weight with density", amt(t, 2, 1, "cup"), ModeWeight, flour, "236.59 g"},
		{"weight without density falls to metric", amt(t, 1, 1, "cup"), ModeWeight, Rat{}, "236.59 ml"},
		{"weight mass to metric", amt(t, 1, 1, "lb"), ModeWeight, Rat{}, "453.59 g"},
		{"metric leaves count", amt(t, 2, 1, "each"), ModeMetric, Rat{}, "2 each"},
		{"metric leaves other", Amount{Qty: Exact(Int(1)), Unit: OtherUnit("clove")}, ModeMetric, Rat{}, "1 clove"},
		{"metric range", rng(t, 1, 2, "cup"), ModeMetric, Rat{}, "236.59-473.18 ml"},
		{"metric range stays small when min under 1000", rng(t, 3, 5, "cup"), ModeMetric, Rat{}, "709.76-1182.94 ml"},
		{"absent", Amount{Unit: MustUnit("cup")}, ModeMetric, Rat{}, "cup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Display(tt.in, tt.mode, tt.density)
			if err != nil {
				t.Fatal(err)
			}
			if got := got.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDisplayNeverMutatesInput(t *testing.T) {
	in := amt(t, 1, 1, "cup")
	before := in.String()
	for _, m := range []Mode{ModeAsWritten, ModeMetric, ModeWeight} {
		if _, err := Display(in, m, rat(t, 1, 2)); err != nil {
			t.Fatal(err)
		}
	}
	if in.String() != before || in.Unit.Name != "cup" {
		t.Errorf("input changed to %s", in)
	}
}

func TestInvalidUnitFactorIsAnError(t *testing.T) {
	bad := Amount{Qty: Exact(Int(2)), Unit: brokenUnit()}
	good := amt(t, 2, 1, "cup")
	if brokenUnit().Converts() {
		t.Error("a unit without a factor must not report Converts")
	}
	if _, err := Convert(bad, MustUnit("ml")); !errors.Is(err, ErrInvalidUnit) {
		t.Errorf("zero source factor err = %v, want ErrInvalidUnit", err)
	}
	if _, err := Convert(good, brokenUnit()); !errors.Is(err, ErrInvalidUnit) {
		t.Errorf("zero target factor err = %v, want ErrInvalidUnit", err)
	}
	neg := Unit{Name: "x", System: SystemUS, Dimension: DimMass, Factor: Int(-1)}
	if _, err := Convert(amt(t, 1, 1, "g"), neg); !errors.Is(err, ErrInvalidUnit) {
		t.Errorf("negative factor err = %v", err)
	}
	if _, _, err := ToMass(bad, Int(1)); !errors.Is(err, ErrInvalidUnit) {
		t.Errorf("ToMass err = %v", err)
	}
	if _, err := Display(bad, ModeMetric, Rat{}); !errors.Is(err, ErrInvalidUnit) {
		t.Errorf("Display err = %v", err)
	}
	if _, err := Display(bad, ModeWeight, Int(1)); !errors.Is(err, ErrInvalidUnit) {
		t.Errorf("Display weight err = %v", err)
	}
	if _, err := Readable(Amount{Qty: Exact(Int(48)), Unit: Unit{Name: "tsp", System: SystemUS, Dimension: DimVolume}}); !errors.Is(err, ErrInvalidUnit) {
		t.Errorf("Readable err = %v", err)
	}
	if _, err := Round(Amount{Qty: Exact(Int(1)), Unit: Unit{Name: "g", System: SystemMetric, Dimension: DimMass}}); !errors.Is(err, ErrInvalidUnit) {
		t.Errorf("Round err = %v", err)
	}
	if _, err := Round(bad); !errors.Is(err, ErrInvalidUnit) {
		t.Errorf("Round US err = %v", err)
	}
	// Unit-less and other units remain valid.
	if err := OtherUnit("pinch").Validate(); err != nil {
		t.Error(err)
	}
	if err := (Unit{}).Validate(); err != nil {
		t.Error(err)
	}
}
