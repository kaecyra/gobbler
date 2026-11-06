package shopping_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

type fixture struct {
	t      *testing.T
	d      *sql.DB
	svc    *apishop.Service
	cat    *catalog.Store
	rec    *store.Store
	ann    apishop.Caller // member of the Smiths
	jo     apishop.Caller // member of the Joneses
	flour  int64
	recipe int64 // a Smiths recipe
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	hs := household.New(d)
	smiths, err := hs.Create(ctx, "Smiths", "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	joneses, err := hs.Create(ctx, "Joneses", "jo@example.com")
	if err != nil {
		t.Fatal(err)
	}
	cat, rec := catalog.New(d), store.New(d)
	f := &fixture{
		t: t, d: d, cat: cat, rec: rec,
		ann: apishop.Caller{Person: "ann@example.com", Household: smiths},
		jo:  apishop.Caller{Person: "jo@example.com", Household: joneses},
		svc: apishop.New(apishop.Deps{Lists: apishop.NewStore(d, rec, cat), Recipes: rec, Catalog: cat, Members: hs}),
	}
	as := household.WithContext(verified(t, "ann@example.com"), smiths)
	ing, err := cat.CreateIngredient(as, catalog.Ingredient{Name: "zz api flour", Aisle: "baking"})
	if err != nil {
		t.Fatal(err)
	}
	f.flour = ing.ID
	f.recipe, err = rec.Create(as, recipe.Recipe{
		Name: "Zz Api Bread",
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{{
				Ingredient: recipe.IngredientID(ing.ID), Rule: quantity.RuleLinear, Raw: "2 cups zz api flour",
				Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(2)), Unit: quantity.MustUnit("cup")},
			}},
			Steps: []recipe.Step{{ID: "s1", Text: "Bake."}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Nobody who is not a verified member of a household gets an operation done,
// whatever the context carries (ADR-00018).
func TestEveryOperationRefusesANonMember(t *testing.T) {
	f := newFixture(t)
	list, err := f.svc.CreateList(context.Background(), f.ann, "Week")
	if err != nil {
		t.Fatal(err)
	}
	// The context names the Smiths: the Caller, not the context, decides.
	ctx := household.WithContext(context.Background(), f.ann.Household)
	one := quantity.Int(1)
	ops := map[string]func(c apishop.Caller) error{
		"Lists":              func(c apishop.Caller) error { _, err := f.svc.Lists(ctx, c); return err },
		"CreateList":         func(c apishop.Caller) error { _, err := f.svc.CreateList(ctx, c, "x"); return err },
		"GetList":            func(c apishop.Caller) error { _, err := f.svc.GetList(ctx, c, list.ID); return err },
		"Load":               func(c apishop.Caller) error { _, err := f.svc.Load(ctx, c, list.ID); return err },
		"Regenerate":         func(c apishop.Caller) error { _, err := f.svc.Regenerate(ctx, c, list.ID); return err },
		"AddEntry":           func(c apishop.Caller) error { _, err := f.svc.AddEntry(ctx, c, list.ID, f.recipe, one); return err },
		"SetEntryMultiplier": func(c apishop.Caller) error { return f.svc.SetEntryMultiplier(ctx, c, 1, one) },
		"RemoveEntry":        func(c apishop.Caller) error { return f.svc.RemoveEntry(ctx, c, 1) },
		"SetIncludeOptional": func(c apishop.Caller) error { return f.svc.SetIncludeOptional(ctx, c, list.ID, true) },
		"AddManualItem": func(c apishop.Caller) error {
			_, err := f.svc.AddManualItem(ctx, c, list.ID, apishop.ManualItem{Text: "soap"})
			return err
		},
		"RemoveManualItem": func(c apishop.Caller) error { return f.svc.RemoveManualItem(ctx, c, 1) },
		"SetChecked":       func(c apishop.Caller) error { return f.svc.SetChecked(ctx, c, list.ID, "k", true) },
		"Pantry":           func(c apishop.Caller) error { _, err := f.svc.Pantry(ctx, c); return err },
		"AddToPantry":      func(c apishop.Caller) error { return f.svc.AddToPantry(ctx, c, recipe.IngredientID(f.flour)) },
		"RemoveFromPantry": func(c apishop.Caller) error { return f.svc.RemoveFromPantry(ctx, c, recipe.IngredientID(f.flour)) },
		"Ingredients":      func(c apishop.Caller) error { _, err := f.svc.Ingredients(ctx, c); return err },
		"Ingredient":       func(c apishop.Caller) error { _, err := f.svc.Ingredient(ctx, c, f.flour); return err },
		"Units":            func(c apishop.Caller) error { _, err := f.svc.Units(ctx, c); return err },
		"Recipes":          func(c apishop.Caller) error { _, err := f.svc.Recipes(ctx, c); return err },
		"AddToActiveList":  func(c apishop.Caller) error { _, err := f.svc.AddToActiveList(ctx, c, f.recipe, one); return err },
	}
	callers := map[string]apishop.Caller{
		"zero caller":     {},
		"no household":    {Person: "ann@example.com"},
		"no person":       {Household: f.ann.Household},
		"household no id": {Person: "ann@example.com", Household: household.Household{Name: "Smiths"}},
	}
	for op, call := range ops {
		for who, c := range callers {
			if err := call(c); !errors.Is(err, apishop.ErrNotMember) {
				t.Errorf("%s as %s: err = %v, want ErrNotMember", op, who, err)
			}
		}
	}
	if lists, err := f.svc.Lists(context.Background(), f.ann); err != nil || len(lists) != 1 {
		t.Errorf("a refused CreateList must not write: lists = %v, err = %v", lists, err)
	}
}

// A member of another household cannot reach a list: it is not found, never
// forbidden, so its existence is not disclosed (ADR-00018). The Caller's
// household wins over the one on the context.
func TestAnotherHouseholdsListIsNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := household.WithContext(context.Background(), f.ann.Household)
	list, err := f.svc.CreateList(ctx, f.ann, "Secret week")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Load(ctx, f.jo, list.ID); !errors.Is(err, apishop.ErrNotFound) {
		t.Errorf("Load as the other household = %v, want ErrNotFound", err)
	}
	if err := f.svc.SetIncludeOptional(ctx, f.jo, list.ID, true); !errors.Is(err, apishop.ErrNotFound) {
		t.Errorf("SetIncludeOptional as the other household = %v, want ErrNotFound", err)
	}
	if lists, err := f.svc.Lists(ctx, f.jo); err != nil || len(lists) != 0 {
		t.Errorf("Lists as the other household = %v, %v, want none", lists, err)
	}
}

func TestListLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.CreateList(ctx, f.ann, "  "); !errors.Is(err, apishop.ErrInvalid) {
		t.Errorf("CreateList with a blank name = %v, want ErrInvalid", err)
	}
	list, err := f.svc.CreateList(ctx, f.ann, "Week")
	if err != nil {
		t.Fatal(err)
	}
	var changes []int64
	f.svc.OnChange(func(c apishop.Change) { changes = append(changes, c.ListID) })

	if _, err := f.svc.AddEntry(ctx, f.ann, list.ID, f.recipe, quantity.Int(2)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddEntry(ctx, f.ann, list.ID, f.recipe, quantity.Int(1)); !errors.Is(err, apishop.ErrDuplicate) {
		t.Errorf("adding the recipe twice = %v, want ErrDuplicate", err)
	}
	if _, err := f.svc.AddManualItem(ctx, f.ann, list.ID, apishop.ManualItem{}); !errors.Is(err, apishop.ErrInvalidManualItem) {
		t.Errorf("an empty manual item = %v, want ErrInvalidManualItem", err)
	}
	if _, err := f.svc.AddManualItem(ctx, f.ann, list.ID, apishop.ManualItem{Text: "paper towels"}); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Regenerate(ctx, f.ann, list.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 || len(view.Manual) != 1 {
		t.Fatalf("view has %d entries and %d manual items, want 1 and 1", len(view.Entries), len(view.Manual))
	}
	var key apishop.ItemKey
	for _, a := range view.Aisles {
		for _, it := range a.Items {
			if it.Name == "zz api flour" {
				key = it.Key
			}
		}
	}
	if key == "" {
		t.Fatalf("the flour is not on the list: %+v", view.Aisles)
	}
	if err := f.svc.SetChecked(ctx, f.ann, list.ID, key, true); err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 {
		t.Error("OnChange heard nothing about the edits")
	}
	if err := f.svc.RemoveManualItem(ctx, f.ann, view.Manual[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RemoveEntry(ctx, f.ann, view.Entries[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestPantryAndCatalogReads(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.svc.AddToPantry(ctx, f.ann, recipe.IngredientID(f.flour)); err != nil {
		t.Fatal(err)
	}
	ids, err := f.svc.Pantry(ctx, f.ann)
	if err != nil || len(ids) != 1 || int64(ids[0]) != f.flour {
		t.Fatalf("pantry = %v, %v, want the flour", ids, err)
	}
	if ing, err := f.svc.Ingredient(ctx, f.ann, f.flour); err != nil || ing.Name != "zz api flour" {
		t.Errorf("Ingredient = %v, %v", ing.Name, err)
	}
	// The pantry is the household's own.
	if ids, err := f.svc.Pantry(ctx, f.jo); err != nil || len(ids) != 0 {
		t.Errorf("the other household's pantry = %v, %v, want empty", ids, err)
	}
	if err := f.svc.RemoveFromPantry(ctx, f.ann, recipe.IngredientID(f.flour)); err != nil {
		t.Fatal(err)
	}
	if rs, err := f.svc.Recipes(ctx, f.ann); err != nil || len(rs) != 1 {
		t.Errorf("Recipes = %v, %v, want the one recipe", rs, err)
	}
	if rs, err := f.svc.Recipes(ctx, f.jo); err != nil || len(rs) != 0 {
		t.Errorf("the other household's recipes = %v, %v, want none", rs, err)
	}
}

func TestAddToActiveList(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.AddToActiveList(ctx, f.ann, f.recipe, quantity.Int(1)); !errors.Is(err, apishop.ErrNoList) {
		t.Fatalf("with no list = %v, want ErrNoList", err)
	}
	if _, err := f.svc.CreateList(ctx, f.ann, "Old"); err != nil {
		t.Fatal(err)
	}
	active, err := f.svc.CreateList(ctx, f.ann, "Active")
	if err != nil {
		t.Fatal(err)
	}
	id, err := f.svc.AddToActiveList(ctx, f.ann, f.recipe, quantity.Int(2))
	if err != nil || id != active.ID {
		t.Fatalf("AddToActiveList = %d, %v, want the newest list %d", id, err, active.ID)
	}
	// A second tap changes the multiplier instead of failing as a duplicate.
	if _, err := f.svc.AddToActiveList(ctx, f.ann, f.recipe, quantity.Int(3)); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Load(ctx, f.ann, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 || view.Entries[0].Multiplier.String() != "3" {
		t.Errorf("entries = %+v, want the recipe once at 3", view.Entries)
	}
	// A recipe of another household is not found, whether or not the caller has a list.
	if _, err := f.svc.AddToActiveList(ctx, f.jo, f.recipe, quantity.Int(1)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("another household's recipe = %v, want store.ErrNotFound", err)
	}
}

// verified returns a context carrying the verified identity of email, the way
// the identity middleware leaves it: the recipe store records who wrote.
func verified(t *testing.T, email string) context.Context {
	t.Helper()
	a, err := auth.New(config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DevAuthBypass: true}})
	if err != nil {
		t.Fatal(err)
	}
	var got context.Context
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(auth.DevIdentityHeader, email)
	a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	if got == nil {
		t.Fatal("auth middleware did not pass the request on")
	}
	return got
}

// A real person who belongs to household A cannot act as household B, however
// B's Caller was assembled (ADR-00018).
func TestAMemberOfAnotherHouseholdIsRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	list, err := f.svc.CreateList(ctx, f.ann, "Smiths week")
	if err != nil {
		t.Fatal(err)
	}
	// Jo is a real member of the Joneses, naming the Smiths.
	intruder := apishop.Caller{Person: f.jo.Person, Household: f.ann.Household}
	if _, err := f.svc.Load(ctx, intruder, list.ID); !errors.Is(err, apishop.ErrNotMember) {
		t.Errorf("Load as Jo naming the Smiths = %v, want ErrNotMember", err)
	}
	if _, err := f.svc.CreateList(ctx, intruder, "planted"); !errors.Is(err, apishop.ErrNotMember) {
		t.Errorf("CreateList as Jo naming the Smiths = %v, want ErrNotMember", err)
	}
	// A person who is in no household at all.
	stranger := apishop.Caller{Person: "nobody@example.com", Household: f.ann.Household}
	if _, err := f.svc.Lists(ctx, stranger); !errors.Is(err, apishop.ErrNotMember) {
		t.Errorf("Lists as a stranger = %v, want ErrNotMember", err)
	}
	if lists, err := f.svc.Lists(ctx, f.ann); err != nil || len(lists) != 1 {
		t.Errorf("the Smiths' lists = %v, %v, want just the one", lists, err)
	}
}
