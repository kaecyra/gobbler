// Command seed fills a fresh data directory with the recipe the recipe-view
// e2e spec reads, and a second one with meanwhile guidance, then prints their ids as JSON. It is test tooling (ADR-00013):
// it goes through the real store and catalog packages, so the spec exercises
// the same read path as production. Usage: seed <data-dir>.
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
	density := 0.5
	flour, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e flour", Aisle: "baking", DensityGPerML: &density})
	if err != nil {
		return err
	}
	sugar, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e sugar", Aisle: "baking"})
	if err != nil {
		return err
	}
	egg, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e egg", Aisle: "dairy"})
	if err != nil {
		return err
	}
	bay, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e bay leaf", Aisle: "spices"})
	if err != nil {
		return err
	}
	pan, err := cat.CreateEquipment(ctx, catalog.Equipment{Name: "e2e 9x13 pan"})
	if err != nil {
		return err
	}

	r := recipe.Recipe{
		Name:     "E2E Sponge",
		Servings: quantity.Exact(mustRat("4")),
		Yield:    recipe.Yield{Qty: quantity.Exact(mustRat("1")), Unit: "cake"},
		Times:    recipe.Times{Prep: 20 * time.Minute, Cook: 45 * time.Minute},
		Components: []recipe.Component{{
			Name: "Batter",
			Lines: []recipe.IngredientLine{
				line("1 1/3", "cup", flour.ID, quantity.RuleLinear),
				line("1/3", "cup", sugar.ID, quantity.RuleLinear),
				line("2", "", egg.ID, quantity.RuleLinear),
				line("1", "", bay.ID, quantity.RuleFixed),
			},
			Steps: []recipe.Step{
				{ID: "mix", Text: "Mix everything.", Duration: 10 * time.Minute},
				{ID: "bake", Text: "Bake until golden.", Duration: 45 * time.Minute, Passive: true, DependsOn: []recipe.StepID{"mix"}},
			},
		}},
		Equipment: []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(pan.ID)}},
	}
	id, err := store.New(d).Create(ctx, r)
	if err != nil {
		return err
	}

	// The meanwhile recipe: while the dough rises, the sauce can be made.
	pizza := recipe.Recipe{
		Name: "E2E Pizza",
		Components: []recipe.Component{
			{Name: "Dough", Steps: []recipe.Step{
				{ID: "mix", Text: "Mix the dough.", Duration: 10 * time.Minute},
				{ID: "rise", Text: "Let the dough rise.", Duration: time.Hour, Passive: true, DependsOn: []recipe.StepID{"mix"}},
				{ID: "bake", Text: "Bake the pizza.", Duration: 15 * time.Minute, DependsOn: []recipe.StepID{"rise"}},
			}},
			{Name: "Sauce", Steps: []recipe.Step{
				{ID: "chop", Text: "Chop the tomatoes.", Duration: 5 * time.Minute},
				{ID: "simmer", Text: "Simmer the sauce.", Duration: 20 * time.Minute, DependsOn: []recipe.StepID{"chop"}},
			}},
			{Name: "Finish", Steps: []recipe.Step{
				{ID: "serve", Text: "Top and serve.", Duration: time.Minute, DependsOn: []recipe.StepID{"bake", "simmer"}},
			}},
		},
	}
	meanwhileID, err := store.New(d).Create(ctx, pizza)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"recipe": id, "meanwhile": meanwhileID})
}

func line(qty, unit string, ingredient int64, rule quantity.Rule) recipe.IngredientLine {
	a := quantity.Amount{Qty: quantity.Exact(mustRat(qty))}
	if unit != "" {
		a.Unit = quantity.MustUnit(unit)
	}
	return recipe.IngredientLine{Amount: a, Ingredient: recipe.IngredientID(ingredient), Rule: rule}
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
