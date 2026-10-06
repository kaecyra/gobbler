package quantity

import (
	"errors"
	"testing"
)

func TestRatNormalisesAndCompares(t *testing.T) {
	tests := []struct {
		name string
		got  Rat
		want string
	}{
		{"reduces", rat(t, 2, 4), "1/2"},
		{"negative denominator moves sign", rat(t, 1, -2), "-1/2"},
		{"whole", rat(t, 6, 3), "2"},
		{"mixed", rat(t, 3, 2), "1 1/2"},
		{"negative mixed", rat(t, -7, 3), "-2 1/3"},
		{"zero value", Rat{}, "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.got.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
	if !rat(t, 2, 4).Equal(rat(t, 1, 2)) {
		t.Error("2/4 should equal 1/2")
	}
	if rat(t, 1, 3).Cmp(rat(t, 1, 2)) >= 0 {
		t.Error("1/3 should be below 1/2")
	}
	if (Rat{}).Sign() != 0 || !Int(3).IsInt() || rat(t, 1, 2).IsInt() {
		t.Error("Sign/IsInt wrong")
	}
	if rat(t, 6, 4).Num().Int64() != 3 || rat(t, 6, 4).Denom().Int64() != 2 {
		t.Error("Num/Denom not normalised")
	}
}

func TestRatArithmeticIsExact(t *testing.T) {
	third := rat(t, 1, 3)
	if got := third.Add(third).Add(third); !got.Equal(Int(1)) {
		t.Errorf("1/3+1/3+1/3 = %s, want 1", got)
	}
	if got := rat(t, 1, 2).Sub(rat(t, 3, 4)); got.String() != "-1/4" {
		t.Errorf("1/2-3/4 = %s", got)
	}
	if got := third.Mul(Int(3)); !got.Equal(Int(1)) {
		t.Errorf("1/3*3 = %s, want 1", got)
	}
	got, err := Int(1).Div(Int(3))
	if err != nil || got.String() != "1/3" {
		t.Errorf("1/3 via Div = %s, %v", got, err)
	}
}

func TestRatDivisionByZeroIsAnError(t *testing.T) {
	if _, err := Int(1).Div(Rat{}); !errors.Is(err, ErrDivideByZero) {
		t.Errorf("Div by zero err = %v", err)
	}
	if _, err := NewRat(1, 0); !errors.Is(err, ErrDivideByZero) {
		t.Errorf("NewRat zero denominator err = %v", err)
	}
}

func TestRatOperationsDoNotMutateOperands(t *testing.T) {
	a, b := rat(t, 1, 2), rat(t, 1, 3)
	_ = a.Add(b)
	_ = a.Mul(b)
	_ = a.Sub(b)
	if a.String() != "1/2" || b.String() != "1/3" {
		t.Errorf("operands changed: %s %s", a, b)
	}
}

func TestRatFloorAndDecimal(t *testing.T) {
	tests := []struct {
		in    Rat
		floor string
		dec   string
	}{
		{rat(t, 7, 2), "3", "3.5"},
		{rat(t, -1, 2), "-1", "-0.5"},
		{Int(4), "4", "4"},
		{rat(t, 1, 3), "0", "0.33"},
		{rat(t, 1, 1000), "0", "0"},
	}
	for _, tt := range tests {
		if got := tt.in.Floor().String(); got != tt.floor {
			t.Errorf("Floor(%s) = %s, want %s", tt.in, got, tt.floor)
		}
		if got := tt.in.Decimal(2); got != tt.dec {
			t.Errorf("Decimal(%s) = %s, want %s", tt.in, got, tt.dec)
		}
	}
	if rat(t, 1, 4).Float64() != 0.25 {
		t.Error("Float64 wrong")
	}
}
