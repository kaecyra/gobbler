package shopping

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

// fixture is a real SQLite file migrated as the binary does, with the three
// stores the shopping store sits beside.
type fixture struct {
	t       *testing.T
	d       *sql.DB
	s       *Store
	recipes *store.Store
	catalog *catalog.Store
	ctx     context.Context
	flour   int64
	butter  int64
	egg     int64
	salt    int64
}

func newFixture(t *testing.T) *fixture {
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
	f := &fixture{t: t, d: d, ctx: authedCtx(t), catalog: catalog.New(d), recipes: store.New(d)}
	f.s = New(d, f.recipes, f.catalog)
	f.flour, f.butter = f.ingredient("zz test flour", "baking"), f.ingredient("zz test butter", "dairy")
	f.egg, f.salt = f.ingredient("zz test egg", "dairy"), f.ingredient("zz test salt", "spices")
	return f
}

func (f *fixture) ingredient(name, aisle string) int64 {
	f.t.Helper()
	in, err := f.catalog.CreateIngredient(f.ctx, catalog.Ingredient{Name: name, Aisle: aisle})
	if err != nil {
		f.t.Fatalf("CreateIngredient(%q): %v", name, err)
	}
	return in.ID
}

// authedCtx returns a context carrying the identity the real auth middleware
// stores (dev bypass), which recipe writes require.
func authedCtx(t *testing.T) context.Context {
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

// ing is one resolved recipe line: n/d of unit of ingredient id.
func (f *fixture) ing(id int64, n, d int64, unit string) recipe.IngredientLine {
	return line(recipe.IngredientID(id), amt(f.t, n, d, unit))
}

// createRecipe stores a recipe made of lines and returns its id.
func (f *fixture) createRecipe(name string, lines ...recipe.IngredientLine) int64 {
	f.t.Helper()
	id, err := f.recipes.Create(f.ctx, recipeOf(name, lines...))
	if err != nil {
		f.t.Fatalf("Create recipe %q: %v", name, err)
	}
	return id
}

func recipeOf(name string, lines ...recipe.IngredientLine) recipe.Recipe {
	return recipe.Recipe{Name: name, Components: []recipe.Component{{
		Lines: lines, Steps: []recipe.Step{{ID: "s1", Text: "Do it."}},
	}}}
}

func (f *fixture) updateRecipe(id int64, name string, lines ...recipe.IngredientLine) {
	f.t.Helper()
	if err := f.recipes.Update(f.ctx, id, recipeOf(name, lines...)); err != nil {
		f.t.Fatalf("Update recipe %d: %v", id, err)
	}
}

func (f *fixture) newList(name string) ListInfo {
	f.t.Helper()
	l, err := f.s.CreateList(f.ctx, name)
	if err != nil {
		f.t.Fatalf("CreateList: %v", err)
	}
	return l
}

func (f *fixture) addEntry(list ListInfo, recipeID, mult int64) int64 {
	f.t.Helper()
	id, err := f.s.AddEntry(f.ctx, list.ID, recipeID, quantity.Int(mult))
	if err != nil {
		f.t.Fatalf("AddEntry: %v", err)
	}
	return id
}

func (f *fixture) load(list ListInfo) View {
	f.t.Helper()
	v, err := f.s.Load(f.ctx, list.ID)
	if err != nil {
		f.t.Fatalf("Load: %v", err)
	}
	return v
}

func (f *fixture) regenerate(list ListInfo) View {
	f.t.Helper()
	v, err := f.s.Regenerate(f.ctx, list.ID)
	if err != nil {
		f.t.Fatalf("Regenerate: %v", err)
	}
	return v
}

func (f *fixture) check(list ListInfo, key ItemKey, on bool) {
	f.t.Helper()
	if err := f.s.SetChecked(f.ctx, list.ID, key, on); err != nil {
		f.t.Fatalf("SetChecked(%q, %v): %v", key, on, err)
	}
}

func (f *fixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.d.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func ingKey(id int64) ItemKey { return KeyOf(Item{Ingredient: recipe.IngredientID(id)}) }

// viewItems flattens the aisles of v by item name.
func viewItems(v View) map[string]ViewItem {
	out := map[string]ViewItem{}
	for _, a := range v.Aisles {
		for _, it := range a.Items {
			out[it.Name] = it
		}
	}
	return out
}

func (f *fixture) fixedClock() {
	f.s.now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }
}

func recipeID(id int64) recipe.IngredientID { return recipe.IngredientID(id) }

func quantity3(t *testing.T) quantity.Rat {
	t.Helper()
	return quantity.Int(3)
}

func openRaw(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(context.Background(), config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func (f *fixture) catalogOn(d *sql.DB) *catalog.Store { return catalog.New(d) }
func (f *fixture) recipesOn(d *sql.DB) *store.Store   { return store.New(d) }

func catalogIngredient(name string) catalog.Ingredient {
	return catalog.Ingredient{Name: name, Aisle: "test"}
}
