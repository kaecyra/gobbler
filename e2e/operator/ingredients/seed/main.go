// Command seed fills a fresh data directory for the central ingredient e2e
// spec: two central ingredients and one overlay entry of the dev household. It
// prints their ids as JSON. It is test tooling (ADR-00013): it goes through
// the real catalog store. Usage: seed <data-dir>.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kaecyra/gobbler/e2e/seedutil"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
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
	dev, d, closeDB, err := seedutil.Open(dir)
	if err != nil {
		return err
	}
	defer closeDB()
	c := catalog.New(d)
	// With no household on the context the entries are central.
	central := household.WithoutHousehold(dev)
	usda := int64(990001)
	saffron, err := c.CreateIngredient(central, catalog.Ingredient{Name: "e2e saffron", Aisle: "spices", Aliases: []string{"e2e saffron threads"}, USDAID: &usda, Reviewed: true})
	if err != nil {
		return err
	}
	turmeric, err := c.CreateIngredient(central, catalog.Ingredient{Name: "e2e turmeric", Aisle: "spices", Reviewed: true})
	if err != nil {
		return err
	}
	// The dev household's own entry, which the operator's page never lists.
	pesto, err := c.CreateIngredient(dev, catalog.Ingredient{Name: "e2e dev pesto", Aisle: "condiments", Reviewed: true})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"saffron": saffron.ID, "turmeric": turmeric.ID, "pesto": pesto.ID})
}
