// Command seed fills a fresh data directory with the recipe the editor e2e
// spec edits and prints its id as JSON. It is test tooling (ADR-00013): it goes
// through the real stores, so the spec exercises the production write path.
// Usage: seed <data-dir>.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: seed <data-dir>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	cfg := config.Config{Server: config.Server{ListenAddr: "127.0.0.1:0", DataDir: dir, DevAuthBypass: true}}
	ctx, err := identityContext(cfg)
	if err != nil {
		return err
	}
	d, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := d.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "seed: close database:", err)
		}
	}()
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		return err
	}
	cat := catalog.New(d)
	flour, err := ingredient(ctx, cat, "flour")
	if err != nil {
		return err
	}
	sugar, err := ingredient(ctx, cat, "sugar")
	if err != nil {
		return err
	}
	cup := quantity.MustUnit("cup")
	id, err := store.New(d).CreateWithSnapshots(ctx, recipe.Recipe{
		Name: "E2E Editor Loaf",
		Components: []recipe.Component{
			{
				Name: "Dough",
				Lines: []recipe.IngredientLine{{
					Amount:     quantity.Amount{Qty: quantity.Exact(quantity.Int(2)), Unit: cup},
					Ingredient: recipe.IngredientID(flour.ID), Rule: quantity.RuleLinear, Raw: "2 cups flour",
				}},
				Steps: []recipe.Step{
					{ID: "mix", Text: "Mix the dough.", Duration: 10 * time.Minute},
					{ID: "rest", Text: "Let the dough rest.", Duration: time.Hour, Passive: true, DependsOn: []recipe.StepID{"mix"}},
				},
			},
			{
				Name: "Glaze",
				Lines: []recipe.IngredientLine{{
					Amount:     quantity.Amount{Qty: quantity.Exact(quantity.Int(1)), Unit: cup},
					Ingredient: recipe.IngredientID(sugar.ID), Rule: quantity.RuleLinear,
				}},
				Steps: []recipe.Step{{ID: "glaze", Text: "Glaze the loaf.", DependsOn: []recipe.StepID{"rest"}}},
			},
		},
	}, store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("source text")})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"recipe": id})
}

// ingredient finds a seeded catalog ingredient, failing if there is none.
func ingredient(ctx context.Context, cat *catalog.Store, name string) (catalog.Ingredient, error) {
	i, ok, err := cat.MatchIngredient(ctx, name)
	if err != nil {
		return catalog.Ingredient{}, fmt.Errorf("match ingredient %q: %w", name, err)
	}
	if !ok {
		return catalog.Ingredient{}, fmt.Errorf("the catalog has no ingredient %q", name)
	}
	return i, nil
}

// identityContext returns a context carrying the verified identity the store
// requires on writes. Only the auth middleware can mint one, so the seed runs
// a request through it in dev-bypass mode.
func identityContext(cfg config.Config) (context.Context, error) {
	a, err := auth.New(cfg)
	if err != nil {
		return nil, err
	}
	var got context.Context
	a.Middleware()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Context() })).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if got == nil {
		return nil, fmt.Errorf("auth middleware did not pass the request on")
	}
	return got, nil
}
