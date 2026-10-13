// Command seed fills a fresh data directory with the recipe the journal e2e
// spec writes to and prints its id as JSON. It is test tooling (ADR-00013): it
// goes through the real store, so the spec exercises the production write
// path. Usage: seed <data-dir>.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kaecyra/gobbler/e2e/seedutil"
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
	id, err := store.New(d).Create(ctx, recipe.Recipe{
		Name:       "E2E Journal Loaf",
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "bake", Text: "Bake the loaf."}}}},
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"recipe": id})
}
