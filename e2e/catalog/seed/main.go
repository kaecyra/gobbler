// Command seed fills a fresh data directory with the unreviewed ingredients and
// the recipe the catalog e2e spec works on, and prints their ids as JSON. It is
// test tooling (ADR-00013): it goes through the real catalog and store, so the
// spec exercises the production write path. Usage: seed <data-dir>.
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
	// A pan the operator can mark sized (ADR-00019), used by the recipe so the
	// spec can see the larger-pan hint appear and go.
	tin, err := c.CreateEquipment(ctx, catalog.Equipment{Name: "e2e roasting tin", Reviewed: true})
	if err != nil {
		return err
	}
	out["tin"] = tin.ID
	out["recipe"], err = store.New(d).Create(ctx, recipe.Recipe{
		Name: "E2E Dill Dip",
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{line(out["source"]), line(out["source"])},
			Steps: []recipe.Step{{ID: "mix", Text: "Mix."}},
		}},
		Equipment: []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(tin.ID)}},
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
