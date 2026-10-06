package parse

import (
	"strings"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// lineUnitAliases lists, per canonical unit name, the spellings recipes use
// for it. Canonical names that quantity knows (g, cup, ...) resolve through
// quantity.LookupUnit so the factors stay in one place; the rest are
// non-converting "other" units ("pinch", "clove", "can"). Aliases are matched
// case-insensitively after trimming a trailing dot, except the single letters
// "T" and "t", which differ only by case.
var lineUnitAliases = map[string][]string{
	"g":      {"g", "gram", "grams", "gr"},
	"kg":     {"kg", "kgs", "kilogram", "kilograms"},
	"oz":     {"oz", "ounce", "ounces"},
	"lb":     {"lb", "lbs", "pound", "pounds"},
	"ml":     {"ml", "mls", "milliliter", "milliliters", "millilitre", "millilitres"},
	"l":      {"l", "liter", "liters", "litre", "litres"},
	"tsp":    {"tsp", "tsps", "teaspoon", "teaspoons"},
	"tbsp":   {"tbsp", "tbsps", "tbs", "tbl", "tablespoon", "tablespoons"},
	"fl oz":  {"fl oz", "fluid ounce", "fluid ounces"},
	"cup":    {"c", "cup", "cups"},
	"pint":   {"pt", "pint", "pints"},
	"quart":  {"qt", "quart", "quarts"},
	"gallon": {"gal", "gallon", "gallons"},
	"dozen":  {"dozen"},

	"pinch":   {"pinch", "pinches"},
	"dash":    {"dash", "dashes"},
	"clove":   {"clove", "cloves"},
	"can":     {"can", "cans", "tin", "tins"},
	"jar":     {"jar", "jars"},
	"package": {"package", "packages", "pkg", "pkgs", "packet", "packets"},
	"stick":   {"stick", "sticks"},
	"bunch":   {"bunch", "bunches"},
	"sprig":   {"sprig", "sprigs"},
	"slice":   {"slice", "slices"},
	"head":    {"head", "heads"},
	"stalk":   {"stalk", "stalks"},
	"piece":   {"piece", "pieces"},
	"bag":     {"bag", "bags"},
	"box":     {"box", "boxes"},
	"bottle":  {"bottle", "bottles"},
	"handful": {"handful", "handfuls"},
}

// lineUnitIndex maps a lowercase alias to its unit, built once at start-up.
var lineUnitIndex = lineBuildUnitIndex()

func lineBuildUnitIndex() map[string]quantity.Unit {
	idx := make(map[string]quantity.Unit)
	for canon, aliases := range lineUnitAliases {
		u, ok := quantity.LookupUnit(canon)
		if !ok {
			u = quantity.OtherUnit(canon)
		}
		for _, a := range aliases {
			idx[a] = u
		}
	}
	return idx
}

// lineLookupUnit resolves a unit spelling such as "Tbsp.", "T" or "fl oz".
func lineLookupUnit(s string) (quantity.Unit, bool) {
	switch s {
	case "T":
		return quantity.MustUnit("tbsp"), true
	case "t":
		return quantity.MustUnit("tsp"), true
	}
	s = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
	s = strings.ReplaceAll(s, "fl. oz", "fl oz")
	u, ok := lineUnitIndex[s]
	return u, ok
}
