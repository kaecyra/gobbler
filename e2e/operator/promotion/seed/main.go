// Command seed fills a fresh data directory for the promotion e2e spec: two
// households, each with an overlay ingredient, one of which the first
// household has suggested for the central catalog. It prints their ids as
// JSON. It is test tooling (ADR-00013): it goes through the real catalog and
// household stores. Usage: seed <data-dir>.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/kaecyra/gobbler/e2e/seedutil"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
)

// otherEmail is the second household's member; the dev bypass seeds the same
// email when the server starts, and Seed does nothing for an email that
// already belongs to a household.
const otherEmail = "other@localhost"

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
	hs := household.New(d)
	if err := hs.Seed(dev, "Other household", otherEmail); err != nil {
		return err
	}
	oh, err := hs.ForEmail(dev, otherEmail)
	if err != nil {
		return fmt.Errorf("find the other household: %w", err)
	}
	other := household.WithContext(context.Background(), oh)

	c := catalog.New(d)
	ghee, err := c.CreateIngredient(dev, catalog.Ingredient{Name: "e2e ghee", Aisle: "dairy", Aliases: []string{"e2e clarified butter"}, Reviewed: true})
	if err != nil {
		return err
	}
	if err := c.SuggestIngredient(dev, ghee.ID); err != nil {
		return err
	}
	// The other household's own entry for the same thing: a duplicate.
	dup, err := c.CreateIngredient(other, catalog.Ingredient{Name: "e2e clarified butter", Aisle: "oils", Aliases: []string{"e2e other ghee"}, Reviewed: true})
	if err != nil {
		return err
	}
	// An entry nobody suggested.
	private, err := c.CreateIngredient(dev, catalog.Ingredient{Name: "e2e private sauce", Aisle: "condiments", Reviewed: true})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"ghee": ghee.ID, "duplicate": dup.ID, "private": private.ID})
}
