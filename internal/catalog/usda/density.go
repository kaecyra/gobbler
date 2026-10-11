package usda

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// undeterminedUnit is the measure_unit_id FoodData Central uses for portions
// whose unit is not in the measure_unit table. SR Legacy foods put the unit in
// the modifier column instead ("cup", "tbsp").
const undeterminedUnit = "9999"

// volumeUnitNames maps the spellings FoodData Central uses for a volume unit
// to the canonical name in internal/quantity. Anything not listed (each, slice,
// pat, "cup, chopped") is not a plain volume and yields no density.
var volumeUnitNames = map[string]string{
	"cup": "cup", "cups": "cup",
	"tbsp": "tbsp", "tablespoon": "tbsp", "tablespoons": "tbsp",
	"tsp": "tsp", "teaspoon": "tsp", "teaspoons": "tsp",
	"fl oz": "fl oz", "fluid ounce": "fl oz", "fluid ounces": "fl oz",
	"ml": "ml", "milliliter": "ml", "milliliters": "ml",
	"l": "l", "liter": "l", "liters": "l",
	"pint": "pint", "quart": "quart", "gallon": "gallon",
}

// densityDecimals rounds a stored density so that the same input always
// produces the same stored value, whatever the float arithmetic did.
const densityDecimals = 4

// portionUnit resolves the volume unit of one food_portion record. The unit
// comes from the measure_unit table when the record names one, otherwise from
// the modifier column. A record that carries any further qualifier (a
// portion_description, or a modifier beyond the unit) describes a prepared or
// shaped food ("cup, chopped"), whose weight is not the weight of a cup of the
// ingredient, so it returns false.
func portionUnit(measureName, modifier, description string) (quantity.Unit, bool) {
	measureName = normalizeText(measureName)
	modifier = normalizeText(modifier)
	if normalizeText(description) != "" {
		return quantity.Unit{}, false
	}
	var spelled string
	switch {
	case measureName != "" && measureName != "undetermined":
		if modifier != "" {
			return quantity.Unit{}, false
		}
		spelled = measureName
	default:
		spelled = modifier
	}
	canonical, ok := volumeUnitNames[spelled]
	if !ok {
		return quantity.Unit{}, false
	}
	u, ok := quantity.LookupUnit(canonical)
	if !ok || !u.Converts() || u.Dimension != quantity.DimVolume {
		return quantity.Unit{}, false
	}
	return u, true
}

// densityGPerML converts a portion (amount of unit weighing gramWeight grams)
// to grams per millilitre. It returns false when the portion cannot give a
// positive finite density.
func densityGPerML(amount float64, unit quantity.Unit, gramWeight float64) (float64, bool) {
	ml := amount * unit.Factor.Float64()
	if ml <= 0 || gramWeight <= 0 || math.IsNaN(ml) || math.IsInf(ml, 0) {
		return 0, false
	}
	d := gramWeight / ml
	scale := math.Pow(10, densityDecimals)
	d = math.Round(d*scale) / scale
	if d <= 0 || math.IsInf(d, 0) {
		return 0, false
	}
	return d, true
}

// parseNumber parses a decimal field. An empty field is reported as absent
// rather than as an error, because survey foods legitimately have no amount.
func parseNumber(s string) (v float64, present bool, err error) {
	if s == "" {
		return 0, false, nil
	}
	v, err = strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse number %q: %w", s, err)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false, fmt.Errorf("number %q is not finite", s)
	}
	return v, true, nil
}

func normalizeText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}
