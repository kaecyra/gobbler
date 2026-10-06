package quantity

import "testing"

func TestRoundUSToCommonFractions(t *testing.T) {
	tests := []struct {
		name string
		in   Amount
		want string
	}{
		{"exact third kept", amt(t, 1, 3, "cup"), "1/3 cup"},
		{"0.34 to third", amt(t, 34, 100, "cup"), "1/3 cup"},
		{"0.7 to two thirds", amt(t, 7, 10, "cup"), "2/3 cup"},
		{"0.8 to three quarters", amt(t, 8, 10, "cup"), "3/4 cup"},
		{"0.95 to next whole", amt(t, 95, 100, "cup"), "1 cup"},
		{"1.06 down to whole", amt(t, 106, 100, "cup"), "1 cup"},
		{"2 1/3 kept", amt(t, 7, 3, "tbsp"), "2 1/3 tbsp"},
		{"tie goes up", amt(t, 3, 16, "tsp"), "1/4 tsp"},
		{"tiny never zero", amt(t, 1, 100, "tsp"), "1/8 tsp"},
		{"zero stays zero", amt(t, 0, 1, "tsp"), "0 tsp"},
		{"oz", amt(t, 52, 10, "oz"), "5 1/4 oz"},
		{"count", amt(t, 27, 10, "each"), "2 2/3 each"},
		{"unitless", Amount{Qty: Exact(rat(t, 51, 100))}, "1/2"},
		{"range each bound", Amount{Qty: mustRange(t, rat(t, 34, 100), rat(t, 7, 10)), Unit: MustUnit("cup")}, "1/3-2/3 cup"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Round(tt.in).Rounded.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRoundMetricToSensibleSteps(t *testing.T) {
	tests := []struct {
		name string
		in   Amount
		want string
	}{
		{"under 10 to half", amt(t, 437, 100, "g"), "4.5 g"},
		{"under 100 to whole", amt(t, 4743, 100, "g"), "47 g"},
		{"under 1000 to five", amt(t, 23659, 100, "ml"), "235 ml"},
		{"over 1000 to ten", amt(t, 118294, 100, "ml"), "1180 ml"},
		{"kg uses base steps", amt(t, 2268, 1000, "kg"), "2.27 kg"},
		{"tiny never zero", amt(t, 1, 100, "g"), "0.5 g"},
		{"zero", amt(t, 0, 1, "g"), "0 g"},
		{"range", rng(t, 236, 473, "ml"), "235-475 ml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Round(tt.in).Rounded.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRoundKeepsExactValueAlongside(t *testing.T) {
	in := amt(t, 34, 100, "cup")
	r := Round(in)
	if got := r.Exact.Qty.String(); got != "17/50" {
		t.Errorf("exact = %s, want 17/50", got)
	}
	if r.Rounded.Qty.String() != "1/3" {
		t.Errorf("rounded = %s", r.Rounded.Qty)
	}
	if in.Qty.String() != "17/50" {
		t.Error("input mutated")
	}
}

func TestRoundAbsent(t *testing.T) {
	a := Amount{Unit: OtherUnit("pinch")}
	r := Round(a)
	if !r.Rounded.Qty.IsAbsent() || !r.Exact.Qty.IsAbsent() {
		t.Errorf("got %+v", r)
	}
}

func mustRange(t *testing.T, lo, hi Rat) Quantity {
	t.Helper()
	q, err := NewRange(lo, hi)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
