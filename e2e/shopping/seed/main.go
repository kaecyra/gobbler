// Command seed fills a fresh data directory with the ingredients and recipes the
// shopping e2e spec builds lists from, and prints their ids as JSON. It is test
// tooling (ADR-00013): it goes through the real catalog and store, so the spec
// exercises the production write path. Usage: seed <data-dir>.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"

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
	out := map[string]int64{}
	for key, in := range map[string]catalog.Ingredient{
		"flour": {Name: "e2e flour", Aisle: "baking", Reviewed: true},
		"milk":  {Name: "e2e milk", Aisle: "dairy", Reviewed: true},
		"salt":  {Name: "e2e salt", Aisle: "spices", Reviewed: true},
		"basil": {Name: "e2e basil", Aisle: "produce", Reviewed: true},
	} {
		created, err := cat.CreateIngredient(ctx, in)
		if err != nil {
			return err
		}
		out[key] = created.ID
	}

	cup := quantity.MustUnit("cup")
	line := func(ing int64, n int64, unit quantity.Unit) recipe.IngredientLine {
		l := recipe.IngredientLine{Ingredient: recipe.IngredientID(ing), Rule: quantity.RuleLinear, Raw: "e2e line"}
		if n > 0 {
			l.Amount = quantity.Amount{Qty: quantity.Exact(quantity.Int(n)), Unit: unit}
		}
		return l
	}
	rec := store.New(d)
	create := func(name string, lines ...recipe.IngredientLine) (int64, error) {
		return rec.Create(ctx, recipe.Recipe{
			Name:       name,
			Components: []recipe.Component{{Lines: lines, Steps: []recipe.Step{{ID: "mix", Text: "Mix."}}}},
		})
	}
	if out["pancakes"], err = create("E2E Pancakes",
		line(out["flour"], 1, cup), line(out["milk"], 1, cup), line(out["salt"], 0, quantity.Unit{})); err != nil {
		return err
	}
	if out["waffles"], err = create("E2E Waffles", line(out["flour"], 2, cup), line(out["milk"], 1, cup)); err != nil {
		return err
	}
	garnish := line(out["basil"], 1, cup)
	garnish.Optional = true
	if out["garnish"], err = create("E2E Garnish", line(out["flour"], 1, cup), garnish); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
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
