package parse

import (
	"os"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
)

// lineFakeCatalog is a map-backed IngredientMatcher for tests.
type lineFakeCatalog map[string]IngredientMatch

func (c lineFakeCatalog) MatchIngredient(name string) (IngredientMatch, bool) {
	m, ok := c[name]
	return m, ok
}

// lineLoadCatalog reads one ingredient name per line; IDs follow file order.
func lineLoadCatalog(t *testing.T) lineFakeCatalog {
	t.Helper()
	b, err := os.ReadFile("testdata/lines/catalog.txt")
	if err != nil {
		t.Fatal(err)
	}
	c := lineFakeCatalog{}
	for i, name := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		name = strings.TrimSpace(name)
		c[name] = IngredientMatch{ID: int64(i + 1), Name: name}
	}
	return c
}

func lineTestCatalog() lineFakeCatalog {
	c := lineFakeCatalog{}
	for i, n := range []string{"butter", "egg", "eggs", "garlic", "salt", "ground beef", "beef", "flour", "sugar", "parsley", "tomatoes", "onion", "milk"} {
		c[n] = IngredientMatch{ID: int64(i + 1), Name: n}
	}
	return c
}

func TestIngredientLineAmounts(t *testing.T) {
	tests := []struct {
		in      string
		qty     string
		unit    string
		pkg     string
		name    string
		toTaste bool
	}{
		{"2 eggs", "2", "", "", "eggs", false},
		{"salt to taste", "", "", "", "salt", true},
		{"2-3 cloves garlic", "2-3", "clove", "", "garlic", false},
		{"1 1/2 cups flour", "1 1/2", "cup", "", "flour", false},
		{"1½ cups flour", "1 1/2", "cup", "", "flour", false},
		{"⅔ cup sugar", "2/3", "cup", "", "sugar", false},
		{"1 (14 oz) can tomatoes", "1", "can", "14 oz", "tomatoes", false},
		{"1 can (14 oz) tomatoes", "1", "can", "14 oz", "tomatoes", false},
		{"2 (8-ounce) cans tomatoes", "2", "can", "8 oz", "tomatoes", false},
		{"200g butter", "200", "g", "", "butter", false},
		{"1/2 to 3/4 cup milk", "1/2-3/4", "cup", "", "milk", false},
		{"a pinch of salt", "1", "pinch", "", "salt", false},
		{"pinch of salt", "", "pinch", "", "salt", false},
		{"2 Tbsp. butter", "2", "tbsp", "", "butter", false},
		{"¼ cup sugar", "1/4", "cup", "", "sugar", false},
		{"1 14-ounce can tomatoes", "1", "can", "14 oz", "tomatoes", false},
		{"2 14 oz cans tomatoes", "2", "can", "14 oz", "tomatoes", false},
		{"1,000 g flour", "1000", "g", "", "flour", false},
		{".25 tsp salt", "1/4", "tsp", "", "salt", false},
		{"1 T butter", "1", "tbsp", "", "butter", false},
		{"1 t salt", "1", "tsp", "", "salt", false},
		{"1 fl oz milk", "1", "fl oz", "", "milk", false},
		{"2 cups of milk", "2", "cup", "", "milk", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := IngredientLine(tt.in, lineTestCatalog())
			if q := got.Amount.Qty.String(); q != tt.qty {
				t.Errorf("qty = %q, want %q", q, tt.qty)
			}
			if got.Amount.Unit.Name != tt.unit {
				t.Errorf("unit = %q, want %q", got.Amount.Unit.Name, tt.unit)
			}
			if tt.pkg == "" && got.Amount.HasPackage {
				t.Errorf("unexpected package %v", got.Amount.Package)
			}
			if tt.pkg != "" {
				p := quantity.Amount{Qty: got.Amount.Package.Qty, Unit: got.Amount.Package.Unit}.String()
				if !got.Amount.HasPackage || p != tt.pkg {
					t.Errorf("package = %q (has %v), want %q", p, got.Amount.HasPackage, tt.pkg)
				}
			}
			if got.Name != tt.name {
				t.Errorf("name = %q, want %q", got.Name, tt.name)
			}
			if got.ToTaste != tt.toTaste {
				t.Errorf("to taste = %v, want %v", got.ToTaste, tt.toTaste)
			}
			if got.Confidence != 1 {
				t.Errorf("confidence = %v (%v), want 1", got.Confidence, got.Issues)
			}
		})
	}
}

func TestIngredientLineQualifiers(t *testing.T) {
	tests := []struct {
		in   string
		name string
		want []LineQualifier
	}{
		{"butter, melted", "butter", []LineQualifier{{LineQualState, "melted"}}},
		{"1 cup melted butter", "butter", []LineQualifier{{LineQualState, "melted"}}},
		{"1 onion, finely chopped", "onion", []LineQualifier{{LineQualPreparation, "finely chopped"}}},
		{"1 cup finely chopped fresh parsley", "parsley",
			[]LineQualifier{{LineQualPreparation, "finely chopped"}, {LineQualForm, "fresh"}}},
		{"1 cup butter, melted and cooled", "butter",
			[]LineQualifier{{LineQualState, "melted"}, {LineQualState, "cooled"}}},
		{"1 cup butter, melted, for serving", "butter",
			[]LineQualifier{{LineQualState, "melted"}, {LineQualNote, "for serving"}}},
		{"2 large eggs", "eggs", []LineQualifier{{LineQualNote, "large"}}},
		{"1 cup milk (cold)", "milk", []LineQualifier{{LineQualNote, "cold"}}},
		{"1 cup butter, at room temperature", "butter", []LineQualifier{{LineQualState, "at room temperature"}}},
		// The catalog knows "ground beef", so "ground" stays in the name.
		{"1 lb ground beef", "ground beef", nil},
		// Without a catalog entry the leading word is a qualifier.
		{"1 lb ground chicken", "chicken", []LineQualifier{{LineQualPreparation, "ground"}}},
		// A line that is only a qualifier word keeps it as the name.
		{"1 cup chopped", "chopped", nil},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := IngredientLine(tt.in, lineTestCatalog())
			if got.Name != tt.name {
				t.Errorf("name = %q, want %q", got.Name, tt.name)
			}
			if len(got.Qualifiers) != len(tt.want) {
				t.Fatalf("qualifiers = %+v, want %+v", got.Qualifiers, tt.want)
			}
			for i, w := range tt.want {
				if got.Qualifiers[i] != w {
					t.Errorf("qualifier %d = %+v, want %+v", i, got.Qualifiers[i], w)
				}
			}
		})
	}
}

func TestIngredientLineOptional(t *testing.T) {
	for _, in := range []string{
		"1 cup sugar (optional)", "1 cup sugar, optional", "1 cup sugar [optional]",
		"Optional: 1 cup sugar", "1 cup sugar (optional, for heat)", "1 cup sugar, optional, divided",
	} {
		t.Run(in, func(t *testing.T) {
			got := IngredientLine(in, lineTestCatalog())
			if !got.Optional {
				t.Errorf("Optional = false")
			}
			if got.Name != "sugar" || got.Amount.Qty.String() != "1" || got.Amount.Unit.Name != "cup" {
				t.Errorf("got %q %s %s, want 1 cup sugar", got.Name, got.Amount.Qty, got.Amount.Unit.Name)
			}
		})
	}
	if got := IngredientLine("1 cup sugar", lineTestCatalog()); got.Optional {
		t.Error("Optional = true for a required line")
	}
}

func TestIngredientLineLowConfidence(t *testing.T) {
	c := lineTestCatalog()
	tests := []struct {
		in    string
		issue string
		max   float64
	}{
		{"2 cups dragon fruit", "not matched", 0.7},
		{"2 cupz sugar", "unrecognised unit", 0.4},
		{"3 xyzzy flour", "unrecognised unit", 0.4},
		{"1 3/4/2 cups flour", "unreadable quantity", 0.6},
		{"1 cup sugar, whatever this is", "unrecognised text", 0.95},
		{"butter", "no quantity", 0.95},
		{"1 cup", "no ingredient name", 0.1},
		{"", "empty line", 0},
		{"   ", "empty line", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := IngredientLine(tt.in, c) // must not panic or fail: low confidence is data
			if got.Confidence > tt.max {
				t.Errorf("confidence = %v, want <= %v", got.Confidence, tt.max)
			}
			if !strings.Contains(strings.Join(got.Issues, "; "), tt.issue) {
				t.Errorf("issues = %v, want one containing %q", got.Issues, tt.issue)
			}
		})
	}
}

func TestIngredientLineUnitGuessKeepsName(t *testing.T) {
	got := IngredientLine("2 cupz sugar", lineTestCatalog())
	if got.Name != "cupz sugar" || got.Matched {
		t.Errorf("name %q matched %v, want the name as written, unmatched", got.Name, got.Matched)
	}
}

func TestIngredientLineNilMatcher(t *testing.T) {
	got := IngredientLine("1 cup flour", nil)
	if got.Matched || got.Name != "flour" || got.Confidence != 1 {
		t.Errorf("got %+v, want unmatched flour at confidence 1", got)
	}
}

func TestIngredientLineKeepsRaw(t *testing.T) {
	const raw = "  1  cup   sugar "
	if got := IngredientLine(raw, nil); got.Raw != raw {
		t.Errorf("Raw = %q, want %q", got.Raw, raw)
	}
}

func TestIngredientLineMatchRecorded(t *testing.T) {
	got := IngredientLine("2 cloves garlic", lineTestCatalog())
	if !got.Matched || got.Match.Name != "garlic" || got.Match.ID == 0 {
		t.Errorf("match = %+v matched=%v", got.Match, got.Matched)
	}
}

func TestLookupUnitAliases(t *testing.T) {
	for in, want := range map[string]string{
		"Tbsp.": "tbsp", "tablespoons": "tbsp", "T": "tbsp", "t": "tsp", "TSP": "tsp",
		"fl. oz.": "fl oz", "Cups": "cup", "lbs": "lb", "mL": "ml", "cloves": "clove",
	} {
		u, ok := lineLookupUnit(in)
		if !ok || u.Name != want {
			t.Errorf("lineLookupUnit(%q) = %q, %v; want %q", in, u.Name, ok, want)
		}
	}
	if _, ok := lineLookupUnit("flour"); ok {
		t.Error("flour read as a unit")
	}
}

func TestParseLineAccentedNameIsNotANumber(t *testing.T) {
	got := IngredientLine("Éclairs, halved", nil)
	if got.Name != "éclairs" || got.Confidence != 0.9 {
		t.Errorf("name %q confidence %v (%v), want éclairs at 0.9 (no quantity only)", got.Name, got.Confidence, got.Issues)
	}
	for _, is := range got.Issues {
		if strings.Contains(is, "unreadable") {
			t.Errorf("issue %q: a letter was read as a number", is)
		}
	}
}
