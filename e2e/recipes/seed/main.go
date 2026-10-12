// Command seed adds the recipes the recipe-list e2e spec searches to a data
// directory and prints their ids by name as JSON. It is test tooling (ADR-00013): it
// goes through the real store, so the spec exercises the production write
// path. Usage: seed <data-dir>.
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
	st := store.New(d)
	ids := map[string]int64{}
	for _, r := range fixtures() {
		id, err := st.Create(ctx, r.doc)
		if err != nil {
			return fmt.Errorf("create %q: %w", r.doc.Name, err)
		}
		ids[r.doc.Name] = id
		for _, n := range r.notes {
			if _, err := st.AddNote(ctx, id, n); err != nil {
				return fmt.Errorf("note on %q: %w", r.doc.Name, err)
			}
		}
	}
	return json.NewEncoder(os.Stdout).Encode(ids)
}

type fixture struct {
	doc   recipe.Recipe
	notes []string
}

// fixtures are the recipes the spec searches: each field the index covers is
// reachable by a word only one recipe holds.
func fixtures() []fixture {
	mk := func(name, desc, line string, tags []string, rating *int, prep, cook time.Duration) recipe.Recipe {
		return recipe.Recipe{
			Name: name, Description: desc, Tags: tags, Rating: rating,
			Times: recipe.Times{Prep: prep, Cook: cook},
			Components: []recipe.Component{{
				Lines: []recipe.IngredientLine{{Rule: quantity.RuleLinear, Raw: line}},
				Steps: []recipe.Step{{ID: "go", Text: "Make it."}},
			}},
		}
	}
	four := 4
	return []fixture{
		{doc: mk("Lemon Cake", "Zesty and light", "3 lemons", []string{"dessert", "quick"}, &four, 15*time.Minute, 40*time.Minute)},
		{doc: mk("Tomato Soup", "Warming", "800 g canned tomatoes", []string{"dinner", "vegan"}, nil, 10*time.Minute, 25*time.Minute)},
		{doc: mk("Beef Stew", "Slow and rich", "1 kg beef", []string{"dinner"}, nil, 20*time.Minute, 150*time.Minute), notes: []string{"Needs more thyme"}},
	}
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
