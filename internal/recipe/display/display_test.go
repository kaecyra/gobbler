package display

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

func rat(t *testing.T, s string) quantity.Rat {
	t.Helper()
	r, err := quantity.ParseRat(s)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func servings(t *testing.T, s string) recipe.Recipe {
	t.Helper()
	r := recipe.Recipe{}
	if s != "" {
		r.Servings = quantity.Exact(rat(t, s))
	}
	return r
}

func TestParseOptions(t *testing.T) {
	r := servings(t, "4")
	o, err := ParseOptions("", "", "", r)
	if err != nil || o.Scaled() || o.Mode != quantity.ModeAsWritten || o.Units != "" {
		t.Fatalf("defaults: %+v, %v", o, err)
	}
	o, err = ParseOptions("1/2", "", UnitsWeight, r)
	if err != nil || !o.Mult.Equal(rat(t, "1/2")) || o.Scale != "1/2" || o.Mode != quantity.ModeWeight || o.Units != UnitsWeight {
		t.Fatalf("scale+units: %+v, %v", o, err)
	}
	o, err = ParseOptions("", "2", UnitsMetric, r)
	if err != nil || !o.Mult.Equal(rat(t, "1/2")) || o.Servings != "2" || o.Mode != quantity.ModeMetric {
		t.Fatalf("servings: %+v, %v", o, err)
	}
	if o, err = ParseOptions("", "", UnitsWritten, r); err != nil || o.Units != "" {
		t.Fatalf("explicit as-written: %+v, %v", o, err)
	}
}

func TestParseOptionsRejects(t *testing.T) {
	for name, tc := range map[string]struct{ scale, servings, units, base string }{
		"bad scale":        {scale: "lots", base: "4"},
		"zero scale":       {scale: "0", base: "4"},
		"negative scale":   {scale: "-2", base: "4"},
		"divide by zero":   {scale: "1/0", base: "4"},
		"bad servings":     {servings: "x", base: "4"},
		"zero servings":    {servings: "0", base: "4"},
		"no servings base": {servings: "4"},
		"both":             {scale: "2", servings: "4", base: "4"},
		"unknown units":    {units: "cubits", base: "4"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseOptions(tc.scale, tc.servings, tc.units, servings(t, tc.base)); !errors.Is(err, ErrInvalid) {
				t.Errorf("err = %v, want ErrInvalid", err)
			}
		})
	}
	_, err := ParseOptions("", "4", "", servings(t, ""))
	if !errors.Is(err, ErrNoServingsBase) {
		t.Errorf("err = %v, want ErrNoServingsBase", err)
	}
}

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "", 30 * time.Second: "1 min", 5 * time.Minute: "5 min", time.Hour: "1 h",
		90 * time.Minute: "1 h 30 min", 10 * time.Second: "under 1 min",
	} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestStepNote(t *testing.T) {
	for _, tc := range []struct {
		s    recipe.Step
		want string
	}{
		{recipe.Step{}, ""},
		{recipe.Step{Duration: 20 * time.Minute}, "20 min"},
		{recipe.Step{Passive: true}, "passive"},
		{recipe.Step{Duration: time.Hour, Passive: true}, "1 h, passive"},
	} {
		if got := StepNote(tc.s); got != tc.want {
			t.Errorf("StepNote(%+v) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func dough() recipe.Recipe {
	return recipe.Recipe{
		Name: "Pizza",
		Components: []recipe.Component{
			{Name: "Dough", Steps: []recipe.Step{
				{ID: "mix", Text: "Mix.", Duration: 10 * time.Minute},
				{ID: "rise", Text: "Rise.", Duration: time.Hour, Passive: true, DependsOn: []recipe.StepID{"mix"}},
			}},
			{Name: "Sauce", Steps: []recipe.Step{
				{ID: "chop", Text: "Chop.", Duration: 5 * time.Minute},
				{ID: "simmer", Text: "Simmer.", DependsOn: []recipe.StepID{"chop"}},
			}},
		},
	}
}

func TestStepNumbersAndMeanwhile(t *testing.T) {
	r := dough()
	n := StepNumbers(r)
	if n["mix"] != 1 || n["rise"] != 2 || n["chop"] != 3 || n["simmer"] != 4 {
		t.Fatalf("numbers = %v", n)
	}
	m, err := BuildMeanwhile(r, n)
	if err != nil {
		t.Fatal(err)
	}
	got := m["rise"]
	if len(got) != 1 || got[0].Number != 3 || got[0].Text != "Chop." {
		t.Errorf("guidance for rise = %+v, want only step 3", got)
	}
	if len(m["mix"]) != 0 {
		t.Errorf("a non-passive step has guidance: %+v", m["mix"])
	}
}

func TestBuildMeanwhileRejectsBrokenGraph(t *testing.T) {
	r := dough()
	r.Components[1].Steps[0].DependsOn = []recipe.StepID{"simmer"}
	if _, err := BuildMeanwhile(r, StepNumbers(r)); err == nil {
		t.Fatal("a cyclic step graph must be an error")
	}
}

type fakeCat map[int64]catalog.Ingredient

func (f fakeCat) GetIngredient(_ context.Context, id int64) (catalog.Ingredient, error) {
	i, ok := f[id]
	if !ok {
		return catalog.Ingredient{}, fmt.Errorf("ingredient %d: %w", id, catalog.ErrNotFound)
	}
	return i, nil
}

func (f fakeCat) GetEquipment(_ context.Context, id int64) (catalog.Equipment, error) {
	if id != 1 {
		return catalog.Equipment{}, fmt.Errorf("equipment %d: %w", id, catalog.ErrNotFound)
	}
	return catalog.Equipment{ID: 1, Name: "baking sheet"}, nil
}

func line(t *testing.T, qty, unit string, ing recipe.IngredientID, rule quantity.Rule) recipe.IngredientLine {
	t.Helper()
	a := quantity.Amount{Qty: quantity.Exact(rat(t, qty))}
	if unit != "" {
		a.Unit = quantity.MustUnit(unit)
	}
	return recipe.IngredientLine{Amount: a, Ingredient: ing, Rule: rule}
}

func TestLineBuilder(t *testing.T) {
	d := 0.5
	cat := fakeCat{1: {ID: 1, Name: "flour", DensityGPerML: &d}, 2: {ID: 2, Name: "bay leaf"}}
	ctx := context.Background()

	b := NewLineBuilder(cat, Default())
	got, err := b.Build(ctx, line(t, "1 1/3", "cup", 1, quantity.RuleLinear))
	if err != nil || got.Text != "1 1/3 cup flour" || got.Exact != "" {
		t.Fatalf("as written: %+v, %v", got, err)
	}

	o, err := ParseOptions("3", "", "", servings(t, "4"))
	if err != nil {
		t.Fatal(err)
	}
	b = NewLineBuilder(cat, o)
	if got, _ = b.Build(ctx, line(t, "1 1/3", "cup", 1, quantity.RuleLinear)); got.Text != "4 cup flour" {
		t.Errorf("scaled linear = %q", got.Text)
	}
	if got, _ = b.Build(ctx, line(t, "1", "", 2, quantity.RuleFixed)); got.Text != "1 bay leaf" {
		t.Errorf("fixed = %q", got.Text)
	}

	o, _ = ParseOptions("", "", UnitsWeight, servings(t, "4"))
	b = NewLineBuilder(cat, o)
	if got, _ = b.Build(ctx, line(t, "1 1/3", "cup", 1, quantity.RuleLinear)); !strings.Contains(got.Text, "g flour") || got.Exact == "" {
		t.Errorf("weight mode = %+v, want grams with an exact hover", got)
	}

	opt := line(t, "2", "", 2, quantity.RuleLinear)
	opt.Optional = true
	opt.Qualifiers = []recipe.Qualifier{{Kind: recipe.QualPreparation, Text: "torn"}}
	if got, _ = NewLineBuilder(cat, Default()).Build(ctx, opt); got.Text != "2 bay leaf, torn (optional)" {
		t.Errorf("qualifier and optional = %q", got.Text)
	}
	taste := recipe.IngredientLine{Ingredient: 2, Rule: quantity.RuleToTaste}
	if got, _ = NewLineBuilder(cat, Default()).Build(ctx, taste); got.Text != "bay leaf to taste" {
		t.Errorf("to taste = %q", got.Text)
	}

	un, err := NewLineBuilder(cat, Default()).Build(ctx, recipe.IngredientLine{Raw: "a splash"})
	if err != nil || !un.Unscaled || un.Text != "a splash" {
		t.Errorf("unresolved = %+v, %v", un, err)
	}

	_, err = NewLineBuilder(cat, Default()).Build(ctx, line(t, "1", "", 99, quantity.RuleLinear))
	if err == nil || errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("dangling ingredient err = %v, want an error that does not wrap ErrNotFound", err)
	}
}

func TestResolveEquipment(t *testing.T) {
	ctx := context.Background()
	link := recipe.EquipmentLink{Equipment: 1, Qty: quantity.Exact(rat(t, "2"))}
	if got, err := ResolveEquipment(ctx, fakeCat{}, link); err != nil || got.Text != "2 baking sheet" || got.Name != "baking sheet" {
		t.Errorf("got %+v, %v", got, err)
	}
	if _, err := ResolveEquipment(ctx, fakeCat{}, recipe.EquipmentLink{Equipment: 9}); err == nil || errors.Is(err, catalog.ErrNotFound) {
		t.Errorf("dangling equipment err = %v", err)
	}
}

func TestScaleQty(t *testing.T) {
	o, _ := ParseOptions("3", "", "", servings(t, "4"))
	got, err := ScaleQty(quantity.Exact(rat(t, "4")), o)
	if err != nil || got != "12" {
		t.Errorf("got %q, %v", got, err)
	}
	if got, _ := ScaleQty(quantity.Exact(rat(t, "4")), Default()); got != "4" {
		t.Errorf("unscaled = %q", got)
	}
}

func TestStateURL(t *testing.T) {
	for _, tc := range []struct {
		scale, servings, units string
		extra                  url.Values
		want                   string
	}{
		{"", "", "", nil, "/p"},
		{"1", "", "", nil, "/p"}, // a scale of 1 is the default view
		{"2", "", "", nil, "/p?scale=2"},
		{"", "6", "weight", nil, "/p?servings=6&units=weight"},
		{"1/2", "", "metric", url.Values{"step": {"3"}}, "/p?scale=1%2F2&step=3&units=metric"},
		{"", "", "", url.Values{"step": {"3"}}, "/p?step=3"},
	} {
		if got := StateURL("/p", tc.scale, tc.servings, tc.units, tc.extra); got != tc.want {
			t.Errorf("StateURL(%q,%q,%q,%v) = %q, want %q", tc.scale, tc.servings, tc.units, tc.extra, got, tc.want)
		}
	}
	o, err := ParseOptions("2", "", UnitsMetric, servings(t, "4"))
	if err != nil {
		t.Fatal(err)
	}
	if got := o.URL("/p", nil); got != "/p?scale=2&units=metric" {
		t.Errorf("Options.URL = %q", got)
	}
}
