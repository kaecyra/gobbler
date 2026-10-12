// Command seed fills a fresh data directory with the unreviewed ingredients and
// the recipe the catalog e2e spec works on, and prints their ids as JSON. It is
// test tooling (ADR-00013): it goes through the real catalog and store, so the
// spec exercises the production write path. Usage: seed <data-dir>.
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
	c := catalog.New(d)
	ing := func(name string, reviewed bool) (int64, error) {
		in, err := c.CreateIngredient(ctx, catalog.Ingredient{Name: name, Aisle: "produce", Reviewed: reviewed})
		return in.ID, err
	}
	out := map[string]int64{}
	for key, spec := range map[string]struct {
		name     string
		reviewed bool
	}{
		"edit": {"e2e dill", false}, "source": {"e2e dillweed", false}, "target": {"e2e fresh dill", true},
	} {
		if out[key], err = ing(spec.name, spec.reviewed); err != nil {
			return err
		}
	}
	line := func(id int64) recipe.IngredientLine {
		return recipe.IngredientLine{Ingredient: recipe.IngredientID(id), Rule: quantity.RuleLinear, Raw: "some dill"}
	}
	out["recipe"], err = store.New(d).Create(ctx, recipe.Recipe{
		Name: "E2E Dill Dip",
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{line(out["source"]), line(out["source"])},
			Steps: []recipe.Step{{ID: "mix", Text: "Mix."}},
		}},
	})
	if err != nil {
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
