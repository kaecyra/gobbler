// Command seed fills a fresh data directory with the recipes the cook-mode e2e
// spec cooks through: a pizza with a passive step, meanwhile guidance and
// timers, and a recipe without steps. It prints their ids as JSON. It is test
// tooling (ADR-00013) and goes through the real store and catalog packages.
// Usage: seed <data-dir>.
package main

import (
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
	flour, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e flour", Aisle: "baking"})
	if err != nil {
		return err
	}
	tomato, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e tomato", Aisle: "produce"})
	if err != nil {
		return err
	}
	bowl, err := cat.CreateEquipment(ctx, catalog.Equipment{Name: "e2e bowl"})
	if err != nil {
		return err
	}

	// Steps: 1 mix, 2 rise (passive), 3 bake, 4 chop, 5 simmer, 6 serve.
	pizza := recipe.Recipe{
		Name:     "E2E Pizza",
		Servings: quantity.Exact(mustRat("4")),
		Components: []recipe.Component{
			{Name: "Dough", Lines: []recipe.IngredientLine{line("1 1/3", "cup", flour.ID)}, Steps: []recipe.Step{
				{ID: "mix", Text: "Mix the dough.", Title: "Make the dough", Duration: 10 * time.Minute},
				{ID: "rise", Text: "Let the dough rise.", Duration: time.Hour, Passive: true, DependsOn: []recipe.StepID{"mix"}},
				{ID: "bake", Text: "Bake the pizza.", DependsOn: []recipe.StepID{"rise"}},
			}},
			{Name: "Sauce", Lines: []recipe.IngredientLine{line("2", "", tomato.ID)}, Steps: []recipe.Step{
				{ID: "chop", Text: "Chop the tomatoes.", Duration: 5 * time.Minute},
				{ID: "simmer", Text: "Simmer the sauce.", Duration: 20 * time.Minute, DependsOn: []recipe.StepID{"chop"}},
			}},
			{Name: "Finish", Steps: []recipe.Step{
				{ID: "serve", Text: "Top and serve.", Duration: time.Minute, DependsOn: []recipe.StepID{"bake", "simmer"}},
			}},
		},
		Equipment: []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(bowl.ID), Steps: []recipe.StepID{"mix"}}},
	}
	pizzaID, err := store.New(d).Create(ctx, pizza)
	if err != nil {
		return err
	}
	emptyID, err := store.New(d).Create(ctx, recipe.Recipe{Name: "E2E Water", Components: []recipe.Component{{}}})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"pizza": pizzaID, "empty": emptyID})
}

func line(qty, unit string, ingredient int64) recipe.IngredientLine {
	a := quantity.Amount{Qty: quantity.Exact(mustRat(qty))}
	if unit != "" {
		a.Unit = quantity.MustUnit(unit)
	}
	return recipe.IngredientLine{Amount: a, Ingredient: recipe.IngredientID(ingredient), Rule: quantity.RuleLinear}
}

func mustRat(s string) quantity.Rat {
	r, err := quantity.ParseRat(s)
	if err != nil {
		panic(err)
	}
	return r
}
