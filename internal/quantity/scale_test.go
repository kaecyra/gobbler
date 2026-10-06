package quantity

import (
	"errors"
	"testing"
)

func TestScaleLinearIsExact(t *testing.T) {
	tests := []struct {
		name string
		in   Amount
		mult Rat
		want string
	}{
		{"1/3 cup x 3 is exactly 1 cup", amt(t, 1, 3, "cup"), Int(3), "1 cup"},
		{"half", amt(t, 1, 1, "cup"), rat(t, 1, 2), "1/2 cup"},
		{"1/3 cup stays", amt(t, 1, 3, "cup"), Int(1), "1/3 cup"},
		{"16 tbsp becomes cup", amt(t, 4, 1, "tbsp"), Int(4), "1 cup"},
		{"range", rng(t, 2, 3, "cup"), Int(2), "4-6 cup"},
		{"1/4 tsp x 3 stays tsp", amt(t, 1, 4, "tsp"), Int(3), "3/4 tsp"},
		{"3 tsp x 1 stays 3 tsp", amt(t, 3, 1, "tsp"), Int(1), "3 tsp"},
		{"unitless", Amount{Qty: Exact(Int(2))}, Int(3), "6"},
		{"other unit", Amount{Qty: Exact(Int(2)), Unit: OtherUnit("clove")}, Int(2), "4 clove"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Scale(tt.in, RuleLinear, tt.mult)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScaleFixedAndToTasteIgnoreMultiplier(t *testing.T) {
	r, _ := NewRange(Int(1), Int(2))
	inputs := []Amount{
		amt(t, 1, 1, "each"),
		amt(t, 1, 3, "cup"),
		{Qty: r, Unit: MustUnit("tsp")},
		{Unit: OtherUnit("pinch")}, // salt to taste: absent
	}
	for _, rule := range []Rule{RuleFixed, RuleToTaste} {
		for _, mult := range []Rat{Int(1), Int(3), rat(t, 1, 2), Int(100)} {
			for _, in := range inputs {
				got, err := Scale(in, rule, mult)
				if err != nil {
					t.Fatal(err)
				}
				if got.String() != in.String() || !got.Qty.Equal(in.Qty) {
					t.Errorf("rule %s mult %s: %q changed to %q", rule, mult, in, got)
				}
			}
		}
	}
}

func TestScaleAbsentLinearStaysAbsent(t *testing.T) {
	got, err := Scale(Amount{Unit: OtherUnit("pinch")}, RuleLinear, Int(4))
	if err != nil || !got.Qty.IsAbsent() {
		t.Errorf("got %v, %v", got, err)
	}
}

func TestScaleLeavesPackageSizeAlone(t *testing.T) {
	in := canOf(1)
	got, err := Scale(in, RuleLinear, Int(2))
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "2 (14 oz) can" {
		t.Errorf("got %q", got)
	}
	if in.String() != "1 (14 oz) can" {
		t.Errorf("input mutated: %q", in)
	}
}

func TestScaleRejectsNonPositiveMultiplier(t *testing.T) {
	for _, m := range []Rat{{}, Int(-2)} {
		if _, err := Scale(amt(t, 1, 1, "cup"), RuleLinear, m); !errors.Is(err, ErrInvalidScale) {
			t.Errorf("mult %s err = %v", m, err)
		}
	}
}

func TestServingsFactor(t *testing.T) {
	got, err := ServingsFactor(Int(4), Int(6))
	if err != nil || got.String() != "1 1/2" {
		t.Errorf("4->6 = %s, %v", got, err)
	}
	for _, bad := range [][2]int64{{0, 4}, {4, 0}, {-1, 4}, {4, -1}} {
		if _, err := ServingsFactor(Int(bad[0]), Int(bad[1])); !errors.Is(err, ErrInvalidScale) {
			t.Errorf("%v err = %v", bad, err)
		}
	}
	// Scaling by the factor: 2 cups for 4 servings -> 3 cups for 6.
	got2, _ := Scale(amt(t, 2, 1, "cup"), RuleLinear, got)
	if got2.String() != "3 cup" {
		t.Errorf("got %q", got2)
	}
}

func TestParseRule(t *testing.T) {
	tests := []struct {
		in      string
		want    Rule
		wantErr bool
	}{
		{"", RuleLinear, false},
		{"linear", RuleLinear, false},
		{"fixed", RuleFixed, false},
		{"to_taste", RuleToTaste, false},
		{"exponential", "", true},
	}
	for _, tt := range tests {
		got, err := ParseRule(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("ParseRule(%q) = %q, %v", tt.in, got, err)
		}
	}
}

func TestReadable(t *testing.T) {
	tests := []struct {
		name string
		in   Amount
		want string
	}{
		{"48 tsp is 1 cup", amt(t, 48, 1, "tsp"), "1 cup"},
		{"3 tsp is 1 tbsp", amt(t, 3, 1, "tsp"), "1 tbsp"},
		{"4 tsp stays (1 1/3 tbsp is not kitchen-natural)", amt(t, 4, 1, "tsp"), "4 tsp"},
		{"24 tsp is 8 tbsp not 1/2 cup", amt(t, 24, 1, "tsp"), "8 tbsp"},
		{"64 tsp is 1 1/3 cup", amt(t, 64, 1, "tsp"), "1 1/3 cup"},
		{"1/2 cup stays", amt(t, 1, 2, "cup"), "1/2 cup"},
		{"cups never go to larger units", amt(t, 8, 1, "cup"), "8 cup"},
		{"16 oz is 1 lb", amt(t, 16, 1, "oz"), "1 lb"},
		{"1500 g is 1.5 kg", amt(t, 1500, 1, "g"), "1.5 kg"},
		{"1100 g stays (1.1 kg is not a common fraction)", amt(t, 1100, 1, "g"), "1100 g"},
		{"1000 ml is 1 l", amt(t, 1000, 1, "ml"), "1 l"},
		{"range both sides", rng(t, 3, 6, "tsp"), "1-2 tbsp"},
		{"range min under 1 stays", rng(t, 1, 6, "tsp"), "1-6 tsp"},
		{"other untouched", Amount{Qty: Exact(Int(48)), Unit: OtherUnit("clove")}, "48 clove"},
		{"absent untouched", Amount{Unit: MustUnit("tsp")}, "tsp"},
		{"non-common denominator stays", Amount{Qty: Exact(rat(t, 1000003, 7)), Unit: MustUnit("tsp")}, "142857 4/7 tsp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Readable(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := got.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScaleRejectsUnknownRule(t *testing.T) {
	for _, rule := range []Rule{"linaer", "", "FIXED"} {
		got, err := Scale(amt(t, 1, 1, "cup"), rule, Int(2))
		if !errors.Is(err, ErrUnknownRule) {
			t.Errorf("rule %q err = %v, want ErrUnknownRule", rule, err)
		}
		if got.String() != "" {
			t.Errorf("rule %q returned %q alongside an error", rule, got)
		}
	}
	if _, err := ParseRule("linaer"); !errors.Is(err, ErrUnknownRule) {
		t.Errorf("ParseRule err = %v, want ErrUnknownRule", err)
	}
}

func TestScaleDoesNotShareStateWithInput(t *testing.T) {
	in := canOf(1)
	out, err := Scale(in, RuleLinear, Int(2))
	if err != nil {
		t.Fatal(err)
	}
	out.Package.Qty = Exact(Int(99))
	if !in.Package.Qty.Equal(Exact(Int(14))) {
		t.Error("writing to the result's package changed the input")
	}
}
