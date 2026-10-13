// Command seed fills a fresh data directory with the recipe the sharing e2e
// spec sends: a recipe of the dev household that uses a central ingredient and
// an ingredient (with an alias) from the dev household's own overlay, with a
// private note, a made-it entry and a source snapshot. It prints the recipe id
// as JSON. It is test tooling (ADR-00013): it goes through the real store and
// catalog packages. Usage: seed <data-dir>.
package main

import (
	"encoding/json"
	"fmt"
	"os"

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
	// "onion" is in the central seed; the fruit is only the dev household's.
	onion, ok, err := cat.MatchIngredient(ctx, "onion")
	if err != nil {
		return fmt.Errorf("find the central onion: %w", err)
	}
	if !ok || !onion.Central {
		return fmt.Errorf("the central catalog has no onion (found %v)", ok)
	}
	fruit, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e sender fruit", Aisle: "produce", Aliases: []string{"e2e sender alias"}})
	if err != nil {
		return err
	}

	r := recipe.Recipe{
		Name:   "E2E Shared Pie",
		Rating: ptr(5),
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{
				line("1", onion.ID, "1 onion"),
				line("3", fruit.ID, "3 e2e sender fruit"),
			},
			Steps: []recipe.Step{{ID: "bake", Text: "Bake the pie."}},
		}},
	}
	rs := store.New(d)
	id, err := rs.CreateWithSnapshots(ctx, r, store.SnapshotInput{Kind: store.SnapshotText, Content: []byte("1 onion\n3 e2e sender fruit\nBake the pie.")})
	if err != nil {
		return err
	}
	if _, err := rs.AddNote(ctx, id, "e2e sender private note"); err != nil {
		return err
	}
	if _, err := rs.AddMadeIt(ctx, id, store.MadeIt{Comment: "e2e sender private comment"}); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"recipe": id})
}

func line(qty string, ingredient int64, raw string) recipe.IngredientLine {
	n, err := quantity.ParseRat(qty)
	if err != nil {
		panic(err)
	}
	return recipe.IngredientLine{
		Amount: quantity.Amount{Qty: quantity.Exact(n)}, Ingredient: recipe.IngredientID(ingredient),
		Rule: quantity.RuleLinear, Raw: raw,
	}
}

func ptr[T any](v T) *T { return &v }
