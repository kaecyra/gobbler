// Command seed fills a fresh data directory for the history e2e spec and prints
// the ids as JSON: a recipe that has been edited once (so it has one revision)
// and one that never has. It is test tooling (ADR-00013): it goes through the
// real store and catalog packages. Usage: seed <data-dir>.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/kaecyra/gobbler/e2e/recipeview/seed/devhousehold"
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
	ctx, err = devhousehold.Context(ctx, d)
	if err != nil {
		return err
	}

	cat := catalog.New(d)
	flour, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e flour", Aisle: "baking"})
	if err != nil {
		return err
	}
	pan, err := cat.CreateEquipment(ctx, catalog.Equipment{Name: "e2e loaf pan"})
	if err != nil {
		return err
	}

	s := store.New(d)
	edited, err := s.Create(ctx, loaf("E2E Loaf", "3", flour.ID, pan.ID))
	if err != nil {
		return err
	}
	if err := s.Update(ctx, edited, loaf("E2E Loaf Edited", "5", flour.ID, pan.ID)); err != nil {
		return err
	}
	plain, err := s.Create(ctx, loaf("E2E Plain Loaf", "2", flour.ID, pan.ID))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"edited": edited, "plain": plain})
}

func loaf(name, flourCups string, flour, pan int64) recipe.Recipe {
	return recipe.Recipe{
		Name:     name,
		Servings: quantity.Exact(mustRat("4")),
		Times:    recipe.Times{Prep: 20 * time.Minute, Cook: 45 * time.Minute},
		Components: []recipe.Component{{
			Name: "Dough",
			Lines: []recipe.IngredientLine{{
				Amount:     quantity.Amount{Qty: quantity.Exact(mustRat(flourCups)), Unit: quantity.MustUnit("cup")},
				Ingredient: recipe.IngredientID(flour),
				Qualifiers: []recipe.Qualifier{{Kind: recipe.QualState, Text: "sifted"}},
				Rule:       quantity.RuleLinear,
			}},
			Steps: []recipe.Step{
				{ID: "mix", Text: "Mix the dough.", Duration: 10 * time.Minute},
				{ID: "bake", Text: "Bake until golden.", Duration: 45 * time.Minute, Passive: true, DependsOn: []recipe.StepID{"mix"}},
			},
		}},
		Equipment: []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(pan), Steps: []recipe.StepID{"bake"}}},
	}
}

func mustRat(s string) quantity.Rat {
	r, err := quantity.ParseRat(s)
	if err != nil {
		panic(err)
	}
	return r
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
