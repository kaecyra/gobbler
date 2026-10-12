package shopping

import (
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

const (
	idButter recipe.IngredientID = iota + 1
	idFlour
	idGarlic
	idSalt
	idEgg
	idMystery
	idCan
)

func ptr(f float64) *float64 { return &f }

func testCatalog() map[recipe.IngredientID]IngredientInfo {
	return map[recipe.IngredientID]IngredientInfo{
		idButter:  {Name: "butter", Aisle: "dairy"},
		idFlour:   {Name: "flour", Aisle: "baking", DensityGPerML: ptr(0.5)},
		idGarlic:  {Name: "garlic", Aisle: "produce"},
		idSalt:    {Name: "salt", Aisle: "spices"},
		idEgg:     {Name: "egg", Aisle: "dairy"},
		idMystery: {Name: "mystery"},
		idCan:     {Name: "tomatoes", Aisle: "canned"},
	}
}

func rat(t *testing.T, n, d int64) quantity.Rat {
	t.Helper()
	r, err := quantity.NewRat(n, d)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// amt builds n/d of the named unit; "" is the unit-less amount.
func amt(t *testing.T, n, d int64, unit string) quantity.Amount {
	t.Helper()
	a := quantity.Amount{Qty: quantity.Exact(rat(t, n, d))}
	switch unit {
	case "":
	case "clove", "can":
		a.Unit = quantity.OtherUnit(unit)
	default:
		a.Unit = quantity.MustUnit(unit)
	}
	return a
}

func line(id recipe.IngredientID, a quantity.Amount) recipe.IngredientLine {
	return recipe.IngredientLine{Ingredient: id, Amount: a, Rule: quantity.RuleLinear, Raw: "raw"}
}

func entry(t *testing.T, id int64, name string, mult int64, lines ...recipe.IngredientLine) RecipeEntry {
	t.Helper()
	return RecipeEntry{
		ID:         id,
		Multiplier: quantity.Int(mult),
		Recipe:     recipe.Recipe{Name: name, Components: []recipe.Component{{Lines: lines}}},
	}
}

func items(l List) map[string]Item {
	out := map[string]Item{}
	for _, a := range l.Aisles {
		for _, it := range a.Items {
			out[it.Name] = it
		}
	}
	return out
}

func gen(t *testing.T, in Input) List {
	t.Helper()
	if in.Catalog == nil {
		in.Catalog = testCatalog()
	}
	l, err := Generate(in)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return l
}

func wantAmounts(t *testing.T, it Item, want ...string) {
	t.Helper()
	var got []string
	for _, a := range it.Amounts {
		got = append(got, a.String())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s amounts = %q, want %q", it.Name, got, want)
	}
}

func TestGenerateSumsSameDimensionExactly(t *testing.T) {
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 1, line(idButter, amt(t, 2, 1, "tbsp"))),
		entry(t, 2, "B", 1, line(idButter, amt(t, 1, 2, "cup"))),
	}})
	wantAmounts(t, items(l)["butter"], "10 tbsp")
	if n := len(l.Aisles[0].Items); n != 1 {
		t.Errorf("want one butter item, got %d", n)
	}
}

func TestGenerateSumMovesToReadableUnit(t *testing.T) {
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 1, line(idButter, amt(t, 1, 2, "cup")), line(idButter, amt(t, 8, 1, "tbsp"))),
	}})
	wantAmounts(t, items(l)["butter"], "1 cup")
}

func TestGenerateSumsRanges(t *testing.T) {
	rng := func(lo, hi int64, unit string) quantity.Amount {
		q, err := quantity.NewRange(quantity.Int(lo), quantity.Int(hi))
		if err != nil {
			t.Fatal(err)
		}
		return quantity.Amount{Qty: q, Unit: quantity.MustUnit(unit)}
	}
	tests := []struct {
		name string
		a, b quantity.Amount
		want string
	}{
		{"range plus exact", rng(2, 3, "tbsp"), amt(t, 1, 1, "tbsp"), "3-4 tbsp"},
		{"exact plus range", amt(t, 1, 1, "tbsp"), rng(2, 3, "tbsp"), "3-4 tbsp"},
		{"range plus range", rng(1, 2, "tbsp"), rng(2, 3, "tbsp"), "3-5 tbsp"},
		{"across a unit conversion", rng(1, 2, "tbsp"), amt(t, 1, 2, "cup"), "9-10 tbsp"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := gen(t, Input{Entries: []RecipeEntry{entry(t, 1, "A", 1, line(idButter, tc.a), line(idButter, tc.b))}})
			wantAmounts(t, items(l)["butter"], tc.want)
		})
	}
}

func TestGenerateScaling(t *testing.T) {
	fixed := line(idGarlic, amt(t, 1, 1, "clove"))
	fixed.Rule = quantity.RuleFixed
	taste := line(idSalt, quantity.Amount{})
	taste.Rule = quantity.RuleToTaste
	rng, err := quantity.NewRange(quantity.Int(2), quantity.Int(3))
	if err != nil {
		t.Fatal(err)
	}
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 3,
			line(idButter, amt(t, 1, 3, "cup")),
			fixed,
			taste,
			line(idEgg, quantity.Amount{Qty: rng}),
		),
	}})
	got := items(l)
	wantAmounts(t, got["butter"], "1 cup")
	wantAmounts(t, got["garlic"], "1 clove")
	wantAmounts(t, got["salt"])
	wantAmounts(t, got["egg"], "6-9")
}

func TestGenerateRejectsBadMultiplier(t *testing.T) {
	for name, m := range map[string]quantity.Rat{"zero": {}, "negative": quantity.Int(-1)} {
		e := entry(t, 1, "A", 1, line(idButter, amt(t, 1, 1, "cup")))
		e.Multiplier = m
		if _, err := Generate(Input{Entries: []RecipeEntry{e}, Catalog: testCatalog()}); !errors.Is(err, ErrInvalidMultiplier) {
			t.Errorf("%s: err = %v, want ErrInvalidMultiplier", name, err)
		}
	}
}

func TestGenerateOptionalLines(t *testing.T) {
	opt := line(idGarlic, amt(t, 2, 1, "clove"))
	opt.Optional = true
	e := entry(t, 1, "A", 1, line(idButter, amt(t, 1, 1, "cup")), opt)

	if _, ok := items(gen(t, Input{Entries: []RecipeEntry{e}}))["garlic"]; ok {
		t.Error("optional line present by default")
	}
	l := gen(t, Input{Entries: []RecipeEntry{e}, IncludeOptional: true})
	wantAmounts(t, items(l)["garlic"], "2 clove")
}

func TestGenerateKeepsIncompatibleDimensionsSeparate(t *testing.T) {
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 1, line(idGarlic, amt(t, 2, 1, "cup")), line(idGarlic, amt(t, 3, 1, "clove"))),
		entry(t, 2, "B", 1, line(idGarlic, amt(t, 1, 1, "clove"))),
	}})
	wantAmounts(t, items(l)["garlic"], "2 cup", "4 clove")
}

func TestGenerateVolumePlusMassNeedsDensity(t *testing.T) {
	in := Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 1, line(idFlour, amt(t, 1, 1, "cup")), line(idFlour, amt(t, 100, 1, "g"))),
	}}
	// density 0.5 g/ml: 1 cup is 236.5882365 ml, so 118.29411825 g.
	wantAmounts(t, items(gen(t, in))["flour"], "218.29 g")

	cat := testCatalog()
	cat[idFlour] = IngredientInfo{Name: "flour", Aisle: "baking"}
	in.Catalog = cat
	wantAmounts(t, items(gen(t, in))["flour"], "1 cup", "100 g")
}

func TestGenerateVolumeAloneStaysVolumeEvenWithDensity(t *testing.T) {
	l := gen(t, Input{Entries: []RecipeEntry{entry(t, 1, "A", 1, line(idFlour, amt(t, 1, 1, "cup")))}})
	wantAmounts(t, items(l)["flour"], "1 cup")
}

func TestGenerateCountsAndUnitless(t *testing.T) {
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 1, line(idEgg, amt(t, 2, 1, "")), line(idEgg, amt(t, 1, 1, "dozen"))),
	}})
	wantAmounts(t, items(l)["egg"], "14")
}

func TestGeneratePackagedAmounts(t *testing.T) {
	can := func(n, size int64) quantity.Amount {
		a := amt(t, n, 1, "can")
		a.HasPackage = true
		a.Package = quantity.PackageSize{Qty: quantity.Exact(quantity.Int(size)), Unit: quantity.MustUnit("oz")}
		return a
	}
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 2, line(idCan, can(1, 14))),
		entry(t, 2, "B", 1, line(idCan, can(1, 14)), line(idCan, can(1, 28))),
	}})
	wantAmounts(t, items(l)["tomatoes"], "3 (14 oz) can", "1 (28 oz) can")
}

func TestGeneratePantry(t *testing.T) {
	entries := []RecipeEntry{entry(t, 1, "A", 1,
		line(idButter, amt(t, 1, 1, "cup")),
		line(idSalt, amt(t, 1, 1, "tsp")),
		line(idSalt, amt(t, 1, 1, "tsp")),
	)}
	pantry := map[recipe.IngredientID]bool{idSalt: true}

	t.Run("dropped and listed separately", func(t *testing.T) {
		l := gen(t, Input{Entries: entries, Pantry: pantry})
		if _, ok := items(l)["salt"]; ok {
			t.Error("pantry item on the list")
		}
		if len(l.Pantry) != 1 || l.Pantry[0].Name != "salt" {
			t.Fatalf("Pantry = %+v", l.Pantry)
		}
		wantAmounts(t, l.Pantry[0], "2 tsp")
	})
	t.Run("added back", func(t *testing.T) {
		l := gen(t, Input{Entries: entries, Pantry: pantry, AddBack: map[recipe.IngredientID]bool{idSalt: true}})
		wantAmounts(t, items(l)["salt"], "2 tsp")
		if len(l.Pantry) != 0 {
			t.Errorf("Pantry = %+v, want empty", l.Pantry)
		}
	})
	t.Run("manual item for a pantry ingredient is kept", func(t *testing.T) {
		l := gen(t, Input{Entries: entries, Pantry: pantry,
			Manual: []ManualItem{{Ingredient: idSalt, Amount: amt(t, 1, 1, "tsp")}}})
		wantAmounts(t, items(l)["salt"], "1 tbsp")
		if len(l.Pantry) != 0 {
			t.Errorf("Pantry = %+v, want empty", l.Pantry)
		}
	})
	t.Run("optional pantry line is not listed", func(t *testing.T) {
		opt := line(idSalt, amt(t, 1, 1, "tsp"))
		opt.Optional = true
		l := gen(t, Input{Entries: []RecipeEntry{entry(t, 1, "A", 1, opt)}, Pantry: pantry})
		if len(l.Pantry) != 0 || len(l.Aisles) != 0 {
			t.Errorf("got %+v", l)
		}
	})
}

func TestGenerateAisleOrder(t *testing.T) {
	l := gen(t, Input{
		Entries: []RecipeEntry{entry(t, 1, "A", 1,
			line(idGarlic, amt(t, 1, 1, "clove")),
			line(idMystery, amt(t, 1, 1, "")),
			line(idEgg, amt(t, 1, 1, "")),
			line(idButter, amt(t, 1, 1, "cup")),
			line(idFlour, amt(t, 1, 1, "cup")),
		)},
		Manual: []ManualItem{{Text: "paper towels"}},
	})
	var names []string
	for _, a := range l.Aisles {
		var ns []string
		for _, it := range a.Items {
			ns = append(ns, it.Name)
		}
		names = append(names, a.Name+":"+strings.Join(ns, ","))
	}
	want := []string{"baking:flour", "dairy:butter,egg", "produce:garlic", "uncategorised:mystery,paper towels"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("aisles = %q, want %q", names, want)
	}
}

func TestGenerateSortsNamesCaseInsensitively(t *testing.T) {
	l := gen(t, Input{
		Entries: []RecipeEntry{entry(t, 1, "A", 1, line(idMystery, amt(t, 1, 1, "")))},
		Manual:  []ManualItem{{Text: "Zebra treats"}, {Text: "Bags"}, {Text: "apple"}},
	})
	var names []string
	for _, it := range l.Aisles[0].Items {
		names = append(names, it.Name)
	}
	if want := []string{"apple", "Bags", "mystery", "Zebra treats"}; !reflect.DeepEqual(names, want) {
		t.Errorf("names = %q, want %q", names, want)
	}
}

func TestGenerateManualItems(t *testing.T) {
	l := gen(t, Input{
		Entries: []RecipeEntry{entry(t, 1, "Cake", 1, line(idButter, amt(t, 1, 2, "cup")))},
		Manual: []ManualItem{
			{Ingredient: idButter, Amount: amt(t, 2, 1, "tbsp")},
			{Text: "Paper Towels"},
			{Text: "  paper towels "},
		},
	})
	got := items(l)
	wantAmounts(t, got["butter"], "10 tbsp")
	if !got["butter"].Manual || len(got["butter"].Sources) != 1 {
		t.Errorf("butter = %+v, want manual and one source", got["butter"])
	}
	if it, ok := got["Paper Towels"]; !ok || !it.Manual || it.Ingredient != 0 || len(it.Amounts) != 0 {
		t.Errorf("free text item = %+v (present %v)", it, ok)
	}
	if len(l.Aisles[len(l.Aisles)-1].Items) != 1 {
		t.Error("duplicate free text should collapse to one item")
	}
}

func TestGenerateRejectsEmptyManualItem(t *testing.T) {
	_, err := Generate(Input{Manual: []ManualItem{{Text: "  "}}, Catalog: testCatalog()})
	if !errors.Is(err, ErrInvalidManualItem) || !strings.Contains(err.Error(), "manual item 1") {
		t.Errorf("err = %v, want ErrInvalidManualItem naming manual item 1", err)
	}
}

func TestGenerateUnknownIngredientIsAnError(t *testing.T) {
	_, err := Generate(Input{
		Entries: []RecipeEntry{entry(t, 1, "A", 1, line(99, amt(t, 1, 1, "cup")))},
		Catalog: testCatalog(),
	})
	if !errors.Is(err, ErrUnknownIngredient) {
		t.Errorf("err = %v, want ErrUnknownIngredient", err)
	}
	_, err = Generate(Input{Manual: []ManualItem{{Text: "x"}, {Ingredient: 99}}, Catalog: testCatalog()})
	if !errors.Is(err, ErrUnknownIngredient) || !strings.Contains(err.Error(), "manual item 2") {
		t.Errorf("manual: err = %v, want ErrUnknownIngredient naming manual item 2", err)
	}
}

func TestGenerateUnresolvedLineBecomesFreeText(t *testing.T) {
	ln := line(0, amt(t, 1, 1, "cup"))
	ln.Raw = "1 cup dragon fruit"
	l := gen(t, Input{Entries: []RecipeEntry{entry(t, 1, "A", 1, ln)}})
	if len(l.Aisles) != 1 || l.Aisles[0].Name != Uncategorised || l.Aisles[0].Items[0].Name != "1 cup dragon fruit" {
		t.Errorf("got %+v", l.Aisles)
	}
}

func TestGenerateUnresolvedLineKeepsItsScale(t *testing.T) {
	ln := line(0, amt(t, 2, 1, "cup"))
	ln.Raw = "2 cups dragon fruit"
	l := gen(t, Input{Entries: []RecipeEntry{entry(t, 1, "A", 2, ln)}})
	it := l.Aisles[0].Items[0]
	if !it.Unresolved || it.Name != "2 cups dragon fruit" || len(it.Amounts) != 0 || !it.Scale.Equal(quantity.Int(2)) {
		t.Errorf("unresolved item must carry the text, an empty Amounts and the scale: %+v", it)
	}
	text, err := l.Text()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Uncategorised\n- 2 cups dragon fruit (x2)\n"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

func TestGenerateUnresolvedLineWithoutParsedAmountKeepsItsScale(t *testing.T) {
	ln := line(0, quantity.Amount{})
	ln.Raw = "some dragon fruit"
	l := gen(t, Input{Entries: []RecipeEntry{entry(t, 1, "A", 2, ln)}})
	text, err := l.Text()
	if err != nil {
		t.Fatal(err)
	}
	if want := "Uncategorised\n- some dragon fruit (x2)\n"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}

func TestGenerateUnresolvedLinesAreNeverCollapsed(t *testing.T) {
	ln := line(0, amt(t, 2, 1, "cup"))
	ln.Raw = "2 cups dragon fruit"
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 1, "A", 1, ln),
		entry(t, 2, "B", 3, ln),
	}})
	text, err := l.Text()
	if err != nil {
		t.Fatal(err)
	}
	want := "Uncategorised\n- 2 cups dragon fruit\n- 2 cups dragon fruit (x3)\n"
	if text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
	if n := len(l.Aisles[0].Items); n != 2 {
		t.Fatalf("items = %d, want 2", n)
	}
	if l.Aisles[0].Items[0].Sources[0].RecipeID != 1 || l.Aisles[0].Items[1].Sources[0].RecipeID != 2 {
		t.Errorf("items out of input order: %+v", l.Aisles[0].Items)
	}
}

func TestGenerateManualFreeTextKeepsAmount(t *testing.T) {
	l := gen(t, Input{Manual: []ManualItem{
		{Text: "Paper Towels", Amount: amt(t, 2, 1, "")},
		{Text: "paper towels", Amount: amt(t, 1, 1, "")},
	}})
	wantAmounts(t, l.Aisles[0].Items[0], "3")
}

func TestGenerateSources(t *testing.T) {
	l := gen(t, Input{Entries: []RecipeEntry{
		entry(t, 7, "Cake", 1, line(idButter, amt(t, 1, 1, "cup")), line(idButter, amt(t, 1, 1, "tbsp"))),
		entry(t, 9, "Pie", 1, line(idButter, amt(t, 1, 1, "tbsp"))),
	}})
	want := []Source{{7, "Cake"}, {9, "Pie"}}
	if got := items(l)["butter"].Sources; !reflect.DeepEqual(got, want) {
		t.Errorf("sources = %+v, want %+v", got, want)
	}
}

func TestGenerateIsDeterministic(t *testing.T) {
	in := Input{
		Entries: []RecipeEntry{
			entry(t, 1, "A", 2, line(idButter, amt(t, 1, 2, "cup")), line(idFlour, amt(t, 3, 1, "cup")), line(idGarlic, amt(t, 1, 1, "clove"))),
			entry(t, 2, "B", 1, line(idEgg, amt(t, 2, 1, "")), line(idMystery, amt(t, 1, 1, "")), line(idSalt, amt(t, 1, 1, "tsp"))),
		},
		Manual: []ManualItem{{Text: "foil"}, {Text: "bags"}},
		Pantry: map[recipe.IngredientID]bool{idSalt: true},
	}
	first := gen(t, in)
	firstText, err := first.Text()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		again := gen(t, in)
		text, err := again.Text()
		if err != nil {
			t.Fatal(err)
		}
		if text != firstText || !reflect.DeepEqual(again, first) {
			t.Fatalf("run %d differs:\n%s\nvs\n%s", i, text, firstText)
		}
	}
}

// The generation half of the package must stay pure: merge*.go, export*.go and
// doc.go touch no database, network or store (code-style rule, ADR-00009). The
// persistence files (store*.go, list*.go, pantry*.go) are not held to this.
func TestPackageIsPure(t *testing.T) {
	var files []string
	for _, pattern := range []string{"merge*.go", "export*.go", "doc.go"} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	banned := []string{"database/sql", "net/http", "net", "os", "time", "internal/db", "internal/catalog", "internal/web", "internal/store"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, src, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			for _, b := range banned {
				if path == b || strings.HasSuffix(path, "/"+b) {
					t.Errorf("%s imports %s", f, path)
				}
			}
		}
	}
}
