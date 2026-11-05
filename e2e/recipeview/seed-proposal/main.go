// Command seed-proposal fills a fresh data directory with the recipes the
// proposal e2e spec reviews, each with an agent's pending proposal, and prints
// their ids and the proposals' tokens as JSON. It is test tooling (ADR-00013):
// it goes through the real stores, so the spec exercises the production write
// path. Usage: seed-proposal <data-dir>.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/kaecyra/gobbler/e2e/seedutil"
	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: seed-proposal <data-dir>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "seed-proposal:", err)
		os.Exit(1)
	}
}

// result is one seeded recipe and the token of its pending proposal.
type result struct {
	Recipe int64  `json:"recipe"`
	Token  string `json:"token"`
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
	salt, err := ingredient(ctx, cat, "salt")
	if err != nil {
		return err
	}
	st := store.New(d)
	cup := quantity.MustUnit("cup")
	line := func(ing catalog.Ingredient, n int64) recipe.IngredientLine {
		return recipe.IngredientLine{
			Amount:     quantity.Amount{Qty: quantity.Exact(quantity.Int(n)), Unit: cup},
			Ingredient: recipe.IngredientID(ing.ID), Rule: quantity.RuleLinear,
		}
	}
	base := func(name string) recipe.Recipe {
		return recipe.Recipe{
			Name: name, Description: "A plain loaf.",
			Components: []recipe.Component{{
				Lines: []recipe.IngredientLine{line(flour, 2), line(salt, 1)},
				Steps: []recipe.Step{
					{ID: "mix", Text: "Mix the dough.", Duration: 10 * time.Minute},
					{ID: "bake", Text: "Bake the loaf.", Duration: 40 * time.Minute, DependsOn: []recipe.StepID{"mix"}},
				},
			}},
		}
	}
	by := store.Provenance{
		Person: auth.DevEmail, Tool: "propose_recipe_edit", Reason: "Less salt and a longer rest make a better crumb.",
		Credential: &store.Credential{Kind: "api_key", ID: 7, Ref: "gobk_e2e7", Owner: auth.DevEmail, CreatedAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)},
	}

	// seed stores a recipe and an agent's proposal that renames it, changes the
	// flour, drops the salt, rewrites the first step, removes the second and adds
	// a new one.
	seed := func(name string) (result, error) {
		id, err := st.Create(ctx, base(name))
		if err != nil {
			return result{}, err
		}
		next, err := st.Get(ctx, id)
		if err != nil {
			return result{}, err
		}
		next.Name = name + " Revised"
		c := &next.Components[0]
		c.Lines = []recipe.IngredientLine{func() recipe.IngredientLine {
			l := c.Lines[0]
			l.Amount.Qty = quantity.Exact(quantity.Int(3))
			return l
		}()}
		first := c.Steps[0]
		first.Text = "Mix the dough and let it rest."
		c.Steps = []recipe.Step{first, {ID: "new-1", Text: "Shape the loaf.", DependsOn: []recipe.StepID{first.ID}}}
		token, err := st.ProposeEdit(ctx, id, 1, next, by)
		return result{Recipe: id, Token: token}, err
	}

	out := map[string]result{}
	for key, name := range map[string]string{
		"marks": "E2E Marks Loaf", "reject": "E2E Reject Loaf", "accept": "E2E Accept Loaf",
		"stale": "E2E Stale Loaf", "race": "E2E Race Loaf", "restore": "E2E Restore Loaf",
	} {
		if out[key], err = seed(name); err != nil {
			return fmt.Errorf("seed %s: %w", key, err)
		}
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

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
