// Command seed fills a fresh data directory with the recipe the editor e2e
// spec edits and prints its id as JSON. It is test tooling (ADR-00013): it goes
// through the real stores, so the spec exercises the production write path.
// Usage: seed <data-dir>.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/kaecyra/gobbler/e2e/seedutil"
	"github.com/kaecyra/gobbler/internal/catalog"
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
	ctx, d, closeDB, err := seedutil.Open(dir)
	if err != nil {
		return err
	}
	defer closeDB()
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
