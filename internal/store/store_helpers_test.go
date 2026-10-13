package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// newTestDB opens a real SQLite file in a temp directory and applies every
// embedded migration, as the binary does at start-up.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	seedHouseholds(t, d)
	return d
}

// The two households every test database starts with. authedCtx acts in the
// first and otherCtx in the second; they are created in this order on an empty
// database, so their ids are fixed.
const (
	testHouseholdID  = 1
	otherHouseholdID = 2
)

func seedHouseholds(t *testing.T, d *sql.DB) {
	t.Helper()
	hs := household.New(d)
	for i, email := range []string{auth.DevEmail, auth.DevOtherEmail} {
		h, err := hs.Create(context.Background(), fmt.Sprintf("Household %d", i+1), email)
		if err != nil {
			t.Fatalf("create household %d: %v", i+1, err)
		}
		if want := int64(i + 1); h.ID != want {
			t.Fatalf("household %d has id %d", want, h.ID)
		}
	}
}

// newTestStore returns a store over a fresh database, with a clock that
// advances one minute per reading so timestamps are distinct and ordered.
func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	d := newTestDB(t)
	s := New(d)
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		at = at.Add(time.Minute)
		return at
	}
	return s, d
}

// authedCtx returns a context carrying the identity the real auth middleware
// stores (dev bypass, which is the only way to obtain one outside package auth)
// and the first test household, as the membership middleware would.
func authedCtx(t *testing.T) context.Context {
	t.Helper()
	return household.WithContext(identityCtx(t), household.Household{ID: testHouseholdID, Name: "Household 1"})
}

// otherCtx is authedCtx for a member of the second test household.
func otherCtx(t *testing.T) context.Context {
	t.Helper()
	return household.WithContext(identityCtx(t), household.Household{ID: otherHouseholdID, Name: "Household 2"})
}

// noHouseholdCtx carries an identity and no household: a request that bypassed
// the membership middleware.
func noHouseholdCtx(t *testing.T) context.Context { return identityCtx(t) }

func identityCtx(t *testing.T) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:8080", DevAuthBypass: true}})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	var got context.Context
	h := a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil {
		t.Fatal("middleware did not call the handler")
	}
	return got
}

func count(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func qty(t *testing.T, s string) quantity.Quantity {
	t.Helper()
	q, err := quantity.ParseQuantity(s)
	if err != nil {
		t.Fatalf("ParseQuantity(%q): %v", s, err)
	}
	return q
}

func ptr[T any](v T) *T { return &v }

// catalogIDs holds the catalog entries a fixture recipe refers to.
type catalogIDs struct {
	flour, butter, egg, salt int64
	mixer, skillet           int64
}

func seedCatalog(t *testing.T, d *sql.DB) (*catalog.Store, catalogIDs) {
	t.Helper()
	ctx := context.Background()
	c := catalog.New(d)
	ing := func(name string) int64 {
		in, err := c.CreateIngredient(ctx, catalog.Ingredient{Name: name, Aisle: "test"})
		if err != nil {
			t.Fatalf("CreateIngredient(%q): %v", name, err)
		}
		return in.ID
	}
	eq := func(name string) int64 {
		e, err := c.CreateEquipment(ctx, catalog.Equipment{Name: name})
		if err != nil {
			t.Fatalf("CreateEquipment(%q): %v", name, err)
		}
		return e.ID
	}
	return c, catalogIDs{
		flour: ing("zz test flour"), butter: ing("zz test butter"), egg: ing("zz test egg"), salt: ing("zz test salt"),
		mixer: eq("zz test mixer"), skillet: eq("zz test skillet"),
	}
}

// fullRecipe exercises every part of the document: exact, fractional and range
// quantities, count-only and packaged lines, qualifiers of each kind, optional
// and non-linear lines, cross-component step dependencies, equipment tied to
// steps, special times, tags, rating and calories.
func fullRecipe(t *testing.T, ids catalogIDs) recipe.Recipe {
	t.Helper()
	return recipe.Recipe{
		Name:               "Pancakes",
		Description:        "Fluffy.",
		SourceURL:          "https://example.test/pancakes",
		Author:             "A. Cook",
		Yield:              recipe.Yield{Qty: qty(t, "12"), Unit: "pancakes"},
		Servings:           qty(t, "4"),
		CaloriesPerServing: ptr(310),
		Tags:               []string{"breakfast", "quick"},
		Rating:             ptr(4),
		Times: recipe.Times{
			Prep: 10 * time.Minute, Cook: 20 * time.Minute, Total: 45 * time.Minute,
			Special: []recipe.SpecialTime{{Label: recipe.LabelRest, Duration: 15 * time.Minute}, {Label: "overnight fridge", Duration: 8 * time.Hour}},
		},
		Components: []recipe.Component{
			{
				Name: "Batter",
				Lines: []recipe.IngredientLine{
					{
						Amount:     quantity.Amount{Qty: qty(t, "1 1/3"), Unit: quantity.MustUnit("cup")},
						Ingredient: recipe.IngredientID(ids.flour), Rule: quantity.RuleLinear, Raw: "1 1/3 cups flour, sifted",
						Qualifiers: []recipe.Qualifier{{Kind: recipe.QualPreparation, Text: "sifted"}, {Kind: recipe.QualForm, Text: "all-purpose"}},
					},
					{
						Amount:     quantity.Amount{Qty: qty(t, "2"), Unit: quantity.Unit{}},
						Ingredient: recipe.IngredientID(ids.egg), Rule: quantity.RuleLinear, Raw: "2 eggs",
						Qualifiers: []recipe.Qualifier{{Kind: recipe.QualState, Text: "at room temperature"}},
					},
					{
						Amount:     quantity.Amount{Qty: qty(t, "50-60"), Unit: quantity.MustUnit("g")},
						Ingredient: recipe.IngredientID(ids.butter), Rule: quantity.RuleLinear, Raw: "50-60 g butter, melted",
						Qualifiers: []recipe.Qualifier{{Kind: recipe.QualPreparation, Text: "melted"}, {Kind: recipe.QualNote, Text: "preferably homemade"}},
					},
					{
						Amount:     quantity.Amount{Qty: quantity.Absent()},
						Ingredient: recipe.IngredientID(ids.salt), Optional: true, Rule: quantity.RuleToTaste, Raw: "salt to taste",
					},
				},
				Steps: []recipe.Step{
					{ID: "mix", Text: "Mix the batter.", Duration: 5 * time.Minute},
					{ID: "rest", Text: "Let it rest.", Duration: 15 * time.Minute, Passive: true, DependsOn: []recipe.StepID{"mix"}},
				},
			},
			{
				Name: "Topping",
				Lines: []recipe.IngredientLine{
					{
						Amount: quantity.Amount{
							Qty: qty(t, "1"), Unit: quantity.OtherUnit("can"),
							Package: quantity.PackageSize{Qty: qty(t, "14"), Unit: quantity.MustUnit("oz")}, HasPackage: true,
						},
						Ingredient: 0, Rule: quantity.RuleFixed, Raw: "1 (14 oz) can something unresolved",
					},
				},
				Steps: []recipe.Step{
					{ID: "melt", Text: "Melt the topping.", Duration: 3 * time.Minute},
					{ID: "fry", Text: "Fry.", Duration: 12 * time.Minute, DependsOn: []recipe.StepID{"rest", "melt"}},
				},
			},
		},
		Equipment: []recipe.EquipmentLink{
			{Equipment: recipe.EquipmentID(ids.mixer), Qty: quantity.Exact(quantity.Int(1)), Steps: []recipe.StepID{"mix"}},
			{Equipment: recipe.EquipmentID(ids.skillet), Steps: []recipe.StepID{"fry", "melt"}},
		},
	}
}

// simpleRecipe is the smallest valid document.
func simpleRecipe(name string) recipe.Recipe {
	return recipe.Recipe{
		Name: name,
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{{Rule: quantity.RuleLinear, Raw: "water"}},
			Steps: []recipe.Step{{ID: "s1", Text: "Do it."}},
		}},
	}
}

// sameRecipe fails the test when got differs from want in any stored field.
// Documents compare through their exact JSON form (quantities as reduced
// fractions); units are compared field by field, factor included.
func sameRecipe(t *testing.T, want, got recipe.Recipe) {
	t.Helper()
	units := &unitTable{ids: map[string]int64{}, units: map[int64]quantity.Unit{}}
	for i, u := range quantity.Units() {
		units.ids[u.Name], units.units[int64(i+1)] = int64(i+1), u
	}
	units.ids["can"], units.units[1000] = 1000, quantity.OtherUnit("can")
	w, err := encodeDocument(want, units)
	if err != nil {
		t.Fatalf("encode want: %v", err)
	}
	g, err := encodeDocument(got, units)
	if err != nil {
		t.Fatalf("encode got: %v", err)
	}
	if string(w) != string(g) {
		t.Fatalf("document differs\nwant %s\n got %s", w, g)
	}
	if len(want.Components) != len(got.Components) {
		t.Fatalf("components: want %d, got %d", len(want.Components), len(got.Components))
	}
	for ci := range want.Components {
		for li, wl := range want.Components[ci].Lines {
			assertUnit(t, wl.Amount.Unit, got.Components[ci].Lines[li].Amount.Unit)
			if wl.Amount.HasPackage {
				assertUnit(t, wl.Amount.Package.Unit, got.Components[ci].Lines[li].Amount.Package.Unit)
			}
		}
	}
}

func assertUnit(t *testing.T, want, got quantity.Unit) {
	t.Helper()
	if want.Name != got.Name || want.System != got.System || want.Dimension != got.Dimension || !want.Factor.Equal(got.Factor) {
		t.Fatalf("unit: want %+v, got %+v", want, got)
	}
}
