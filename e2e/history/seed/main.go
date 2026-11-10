// Command seed fills a fresh data directory for the history e2e spec and prints
// the ids as JSON: a recipe that has been edited once (so it has one revision)
// one that never has, and one whose latest version was a re-import. It is test tooling (ADR-00013): it goes through the
// real store and catalog packages. Usage: seed <data-dir>.
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
	flour, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e flour", Aisle: "baking"})
	if err != nil {
		return err
	}
	pan, err := cat.CreateEquipment(ctx, catalog.Equipment{Name: "e2e loaf pan"})
	if err != nil {
		return err
	}

	s := store.New(d)
	edited, err := s.Create(ctx, loaf("E2E Loaf", "3", flour.ID, pan.ID))
	if err != nil {
		return err
	}
	if err := s.Update(ctx, edited, loaf("E2E Loaf Edited", "5", flour.ID, pan.ID)); err != nil {
		return err
	}
	plain, err := s.Create(ctx, loaf("E2E Plain Loaf", "2", flour.ID, pan.ID))
	if err != nil {
		return err
	}
	reimported, err := reimport(ctx, s, loaf("E2E Imported Loaf", "2", flour.ID, pan.ID))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"edited": edited, "plain": plain, "reimported": reimported})
}

// reimport stores r as imported from a page, then saves a re-import of it, so
// its history has a revision written by one (ADR-00042).
func reimport(ctx context.Context, s *store.Store, r recipe.Recipe) (int64, error) {
	r.SourceURL = "https://recipes.example.test/loaf"
	id, err := s.CreateWithSnapshots(ctx, r, store.SnapshotInput{Kind: store.SnapshotHTML, Content: []byte("<html>first</html>")})
	if err != nil {
		return 0, err
	}
	token, err := s.ProposeDraft(ctx, store.DraftInput{
		Document:   []byte(fmt.Sprintf(`{"reimport":{"recipe":%d,"base_version":1},"components":[]}`, id)),
		Source:     store.SnapshotInput{Kind: store.SnapshotHTML, Content: []byte("<html>again</html>")},
		Provenance: store.Provenance{Person: "dev@localhost"},
	})
	if err != nil {
		return 0, err
	}
	r.Name = "E2E Imported Loaf, read again"
	if _, err := s.AcceptReimport(ctx, token, r); err != nil {
		return 0, err
	}
	return id, nil
}

func loaf(name, flourCups string, flour, pan int64) recipe.Recipe {
	return recipe.Recipe{
		Name:     name,
		Servings: quantity.Exact(mustRat("4")),
		Times:    recipe.Times{Prep: 20 * time.Minute, Cook: 45 * time.Minute},
		Components: []recipe.Component{{
			Name: "Dough",
			Lines: []recipe.IngredientLine{{
				Amount:     quantity.Amount{Qty: quantity.Exact(mustRat(flourCups)), Unit: quantity.MustUnit("cup")},
				Ingredient: recipe.IngredientID(flour),
				Qualifiers: []recipe.Qualifier{{Kind: recipe.QualState, Text: "sifted"}},
				Rule:       quantity.RuleLinear,
			}},
			Steps: []recipe.Step{
				{ID: "mix", Text: "Mix the dough.", Duration: 10 * time.Minute},
				{ID: "bake", Text: "Bake until golden.", Duration: 45 * time.Minute, Passive: true, DependsOn: []recipe.StepID{"mix"}},
			},
		}},
		Equipment: []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(pan), Steps: []recipe.StepID{"bake"}}},
	}
}

func mustRat(s string) quantity.Rat {
	r, err := quantity.ParseRat(s)
	if err != nil {
		panic(err)
	}
	return r
}
