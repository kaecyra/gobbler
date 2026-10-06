package quantity

import (
	"errors"
	"testing"
)

func TestParseRat(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"2", "2"},
		{"  3  ", "3"},
		{"0", "0"},
		{"1/2", "1/2"},
		{"3/4", "3/4"},
		{"6/4", "1 1/2"},
		{"1 1/2", "1 1/2"},
		{"2  3/4", "2 3/4"},
		{"1-1/2", "1 1/2"},
		{"0.5", "1/2"},
		{".25", "1/4"},
		{"1.5", "1 1/2"},
		{"0.333", "333/1000"},
		{"½", "1/2"},
		{"⅓", "1/3"},
		{"⅔", "2/3"},
		{"¼", "1/4"},
		{"¾", "3/4"},
		{"⅛", "1/8"},
		{"⅞", "7/8"},
		{"1½", "1 1/2"},
		{"2 ¾", "2 3/4"},
		{"1⁄2", "1/2"}, // fraction slash
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseRat(tt.in)
			if err != nil {
				t.Fatalf("ParseRat(%q): %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Errorf("ParseRat(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseRatMalformed(t *testing.T) {
	tests := []string{
		"", "   ", "abc", "-1", "-1/2", "1/", "/2", "1/2/3", "1.2.3", "1,5", "1e3",
		"1 2", "1 1/2 1/2", "1 3/2", "a 1/2", "1.5 1/2", "1-⅓", "1/-2", "+1", "1 /2",
		"x½", "1 ½ ½", "1-1/x", "2.",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			if got, err := ParseRat(in); err == nil {
				t.Errorf("ParseRat(%q) = %s, want error", in, got)
			} else if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrDivideByZero) {
				t.Errorf("ParseRat(%q) error %v is neither malformed nor divide by zero", in, err)
			}
		})
	}
}

func TestParseRatZeroDenominatorIsDivideByZero(t *testing.T) {
	if _, err := ParseRat("1/0"); !errors.Is(err, ErrDivideByZero) {
		t.Errorf("err = %v", err)
	}
}

func TestParseQuantity(t *testing.T) {
	tests := []struct {
		in       string
		wantKind Kind
		want     string
	}{
		{"2", KindExact, "2"},
		{"1 1/2", KindExact, "1 1/2"},
		{"⅓", KindExact, "1/3"},
		{"0.5", KindExact, "1/2"},
		{"1-1/2", KindExact, "1 1/2"}, // mixed number, not a range
		{"2-3", KindRange, "2-3"},
		{"2 - 3", KindRange, "2-3"},
		{"2–3", KindRange, "2-3"},
		{"2—3", KindRange, "2-3"},
		{"2 to 3", KindRange, "2-3"},
		{"1/2-3/4", KindRange, "1/2-3/4"},
		{"½ to ¾", KindRange, "1/2-3/4"},
		{"1 1/2-2", KindRange, "1 1/2-2"},
		{"0.5-1.5", KindRange, "1/2-1 1/2"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			q, err := ParseQuantity(tt.in)
			if err != nil {
				t.Fatalf("ParseQuantity(%q): %v", tt.in, err)
			}
			if q.Kind() != tt.wantKind || q.String() != tt.want {
				t.Errorf("ParseQuantity(%q) = kind %d %q, want kind %d %q", tt.in, q.Kind(), q.String(), tt.wantKind, tt.want)
			}
		})
	}
}

func TestParseQuantityMalformed(t *testing.T) {
	for _, in := range []string{"", "to taste", "2-", "-3", "2--3", "a-b", "3-2", "1 to", "2 to x"} {
		t.Run(in, func(t *testing.T) {
			if q, err := ParseQuantity(in); err == nil {
				t.Errorf("ParseQuantity(%q) = %v, want error", in, q)
			}
		})
	}
	if _, err := ParseQuantity("3-2"); !errors.Is(err, ErrInvalidRange) {
		t.Errorf("reversed range err = %v, want ErrInvalidRange", err)
	}
}
