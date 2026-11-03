// Command seed fills a fresh data directory for the MCP e2e specs and prints, as
// JSON, the ids and the API key secrets they use. It is test tooling
// (ADR-00013): it goes through the real catalog, store and key store, so the
// specs exercise the production write path. Usage: seed <data-dir>.
//
// The dev households are the ones the dev bypass seeds: DEV and OTHER. The
// secrets printed are throwaway keys for a throwaway database.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kaecyra/gobbler/e2e/seedutil"
	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

const strangerEmail = "stranger@e2e.example"

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

	hs := household.New(d)
	// The same name the dev bypass gives it (devSeeded in internal/web).
	if err := hs.Seed(ctx, "Other household", auth.DevOtherEmail); err != nil {
		return err
	}
	devHousehold, ok := household.FromContext(ctx)
	if !ok {
		return fmt.Errorf("the seed context has no household")
	}
	otherHousehold, err := hs.ForEmail(ctx, auth.DevOtherEmail)
	if err != nil {
		return err
	}

	cat := catalog.New(d)
	flour, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e mcp flour", Aisle: "baking", Reviewed: true})
	if err != nil {
		return err
	}
	rec := store.New(d)
	create := func(c context.Context, ingredient int64, name string) (int64, error) {
		line := recipe.IngredientLine{
			Ingredient: recipe.IngredientID(ingredient), Rule: quantity.RuleLinear, Raw: "2 cups e2e mcp flour",
			Amount: quantity.Amount{Qty: quantity.Exact(quantity.Int(2)), Unit: quantity.MustUnit("cup")},
		}
		return rec.Create(c, recipe.Recipe{
			Name:       name,
			Components: []recipe.Component{{Lines: []recipe.IngredientLine{line}, Steps: []recipe.Step{{ID: "mix", Text: "Mix."}}}},
		})
	}
	pancakes, err := create(ctx, flour.ID, "E2E MCP Pancakes")
	if err != nil {
		return err
	}
	// An ingredient an overlay entry of the other household's own (ADR-00018).
	otherCtx := household.WithContext(ctx, otherHousehold)
	otherFlour, err := cat.CreateIngredient(otherCtx, catalog.Ingredient{Name: "e2e mcp other flour", Aisle: "baking", Reviewed: true})
	if err != nil {
		return err
	}
	secret, err := create(otherCtx, otherFlour.ID, "E2E MCP Secret Stew")
	if err != nil {
		return err
	}

	keys := apikey.New(d)
	out := map[string]any{"pancakes": pancakes, "secret": secret}
	for name, email := range map[string]string{"devKey": auth.DevEmail, "otherKey": auth.DevOtherEmail, "strangerKey": strangerEmail} {
		if out[name], err = keys.CreatePersonal(ctx, email, "e2e "+name, 0); err != nil {
			return err
		}
	}
	if out["householdKey"], err = keys.CreateHousehold(ctx, devHousehold.ID, auth.DevEmail, "e2e household key", 0); err != nil {
		return err
	}
	if out["shortKey"], err = keys.CreatePersonal(ctx, auth.DevEmail, "e2e short", 1); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
