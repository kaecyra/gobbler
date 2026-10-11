package usda

import (
	"math"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

func TestPortionUnit(t *testing.T) {
	cases := []struct {
		name                         string
		measure, modifier, portionDs string
		want                         string // canonical unit, "" for none
	}{
		{"measure unit name", "cup", "", "", "cup"},
		{"measure unit spelled out", "tablespoon", "", "", "tbsp"},
		{"measure unit teaspoon", "teaspoon", "", "", "tsp"},
		{"measure unit liter", "liter", "", "", "l"},
		{"modifier when undetermined", "", "cup", "", "cup"},
		{"modifier abbreviation", "", "tbsp", "", "tbsp"},
		{"modifier fluid ounce", "", "fl oz", "", "fl oz"},
		{"modifier case and spacing", "", "  Cup ", "", "cup"},
		{"undetermined name falls to modifier", "undetermined", "tsp", "", "tsp"},
		{"qualified modifier", "", "cup, chopped", "", ""},
		{"modifier on a named unit", "cup", "sifted", "", ""},
		{"portion description qualifies", "cup", "", "packed", ""},
		{"count unit", "slice", "", "", ""},
		{"mass unit", "", "oz", "", ""},
		{"nothing", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, ok := portionUnit(tc.measure, tc.modifier, tc.portionDs)
			if (tc.want != "") != ok || u.Name != tc.want {
				t.Errorf("portionUnit = (%q, %v), want %q", u.Name, ok, tc.want)
			}
		})
	}
}

func TestDensityGPerML(t *testing.T) {
	cases := []struct {
		name   string
		amount float64
		unit   string
		grams  float64
		want   float64
		ok     bool
	}{
		{"water, one ml", 1, "ml", 1, 1, true},
		{"water, one liter", 1, "l", 1000, 1, true},
		{"cup of flour", 1, "cup", 125, 0.5283, true},
		{"two tablespoons of honey", 2, "tbsp", 42, 1.4202, true},
		{"teaspoon", 1, "tsp", 4.2, 0.8521, true},
		{"fluid ounce", 1, "fl oz", 30, 1.0144, true},
		{"fractional amount", 0.5, "cup", 60, 0.5072, true},
		{"gallon", 1, "gallon", 3785.411784, 1, true},
		{"zero grams", 1, "cup", 0, 0, false},
		{"zero amount", 0, "cup", 100, 0, false},
		{"negative grams", 1, "cup", -5, 0, false},
		{"rounds to zero", 1, "l", 0.00001, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := densityGPerML(tc.amount, quantity.MustUnit(tc.unit), tc.grams)
			if ok != tc.ok || (ok && math.Abs(got-tc.want) > 1e-9) {
				t.Errorf("densityGPerML = (%v, %v), want (%v, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestParseNumber(t *testing.T) {
	if _, present, err := parseNumber(""); present || err != nil {
		t.Errorf("empty = (%v, %v), want absent without error", present, err)
	}
	if v, present, err := parseNumber("2.5"); v != 2.5 || !present || err != nil {
		t.Errorf("2.5 = (%v, %v, %v)", v, present, err)
	}
	for _, bad := range []string{"abc", "NaN", "Inf"} {
		if _, _, err := parseNumber(bad); err == nil {
			t.Errorf("parseNumber(%q) succeeded", bad)
		}
	}
}
