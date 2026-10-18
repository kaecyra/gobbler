// Command seed adds the recipes the library and home e2e specs read to a data
// directory and prints their ids by name as JSON. It is test tooling (ADR-00013): it
// goes through the real store, so the spec exercises the production write
// path. Usage: seed <data-dir>.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	st := store.New(d)
	cat := catalog.New(d)
	now := time.Now()
	ids := map[string]int64{}
	for _, r := range fixtures() {
		doc := r.doc
		for ci := range doc.Components {
			for li := range doc.Components[ci].Lines {
				raw, name, _ := strings.Cut(doc.Components[ci].Lines[li].Raw, " :: ")
				doc.Components[ci].Lines[li].Raw = raw
				ing, found, err := cat.MatchIngredient(ctx, name)
				if err == nil && !found {
					ing, err = cat.CreateIngredient(ctx, catalog.Ingredient{Name: name, Aisle: "e2e"})
				}
				if err != nil {
					return fmt.Errorf("ingredient %q: %w", name, err)
				}
				doc.Components[ci].Lines[li].Ingredient = recipe.IngredientID(ing.ID)
			}
		}
		id, err := st.Create(ctx, doc)
		if err != nil {
			return fmt.Errorf("create %q: %w", doc.Name, err)
		}
		ids[doc.Name] = id
		for _, n := range r.notes {
			if _, err := st.AddNote(ctx, id, n); err != nil {
				return fmt.Errorf("note on %q: %w", doc.Name, err)
			}
		}
		for _, m := range r.made {
			e := store.MadeIt{On: now.AddDate(0, 0, -m.daysAgo), Comment: m.comment}
			if _, err := st.AddMadeIt(ctx, id, e); err != nil {
				return fmt.Errorf("made-it on %q: %w", doc.Name, err)
			}
		}
	}
	return json.NewEncoder(os.Stdout).Encode(ids)
}

type madeAt struct {
	daysAgo int
	comment string
}

type fixture struct {
	doc   recipe.Recipe
	notes []string
	made  []madeAt
}

// fixtures are the recipes the specs read: each field the search index covers is
// reachable by a word only one recipe holds, the meal types, spice levels and
// made-it logs give the cards and panels something to show, and Cardamom Knots
// has enough alternating steps for its ribbon to need merging when narrow.
//
// A line is written "raw text :: ingredient name"; run splits it.
func fixtures() []fixture {
	hands := func(key string, minutes int, kind recipe.StageKind) recipe.Step {
		return recipe.Step{ID: recipe.StepID(key), Text: "Do " + key + ".", Duration: time.Duration(minutes) * time.Minute, Kind: kind}
	}
	wait := func(key string, minutes int, kind recipe.StageKind) recipe.Step {
		s := hands(key, minutes, kind)
		s.Passive = true
		return s
	}
	mk := func(name, desc string, meal recipe.MealType, spice *int, line string, tags []string, rating *int, prep, cook time.Duration, steps ...recipe.Step) recipe.Recipe {
		return recipe.Recipe{
			Name: name, Description: desc, Tags: tags, Rating: rating, MealType: meal, SpiceLevel: spice,
			SourceURL: "https://www.example.test/" + tags[0],
			Times:     recipe.Times{Prep: prep, Cook: cook},
			Components: []recipe.Component{{
				Name:  "Main",
				Lines: []recipe.IngredientLine{{Rule: quantity.RuleLinear, Raw: line}},
				Steps: steps,
			}},
		}
	}
	four, one, two := 4, 1, 2
	// italian marks a recipe's cuisine; Tomato Soup is the one card with one.
	italian := func(r recipe.Recipe) recipe.Recipe {
		r.Cuisine = recipe.CuisineItalian
		return r
	}
	fx := []fixture{
		{
			doc:  mk("Lemon Cake", "Zesty and light", recipe.MealDessert, nil, "3 lemons :: lemons", []string{"dessert", "quick"}, &four, 15*time.Minute, 40*time.Minute, hands("mix", 15, recipe.StagePrep), wait("bake", 40, recipe.StageBake)),
			made: []madeAt{{daysAgo: 200, comment: "Too sweet"}},
		},
		{
			doc:  italian(mk("Tomato Soup", "Warming", recipe.MealMain, &two, "800 g canned tomatoes :: tomatoes", []string{"dinner", "vegan"}, nil, 10*time.Minute, 25*time.Minute, hands("chop", 10, recipe.StagePrep), wait("simmer", 25, recipe.StageSimmer))),
			made: []madeAt{{daysAgo: 30}, {daysAgo: 9, comment: "Added basil"}, {daysAgo: 2}},
		},
		{doc: mk("Beef Stew", "Slow and rich", recipe.MealMain, &one, "1 kg beef :: beef", []string{"dinner"}, nil, 20*time.Minute, 150*time.Minute, hands("brown", 20, recipe.StagePrep), wait("braise", 150, recipe.StageBraise)), notes: []string{"Needs more thyme"}},
		{doc: mk("Slow Ribs", "Falls off the bone", recipe.MealMain, nil, "2 kg ribs :: short ribs", []string{"bbq"}, nil, 20*time.Minute, 240*time.Minute, hands("rub", 20, recipe.StagePrep), wait("roast", 240, recipe.StageRoast))},
		{doc: mk("Cardamom Knots", "Sticky morning buns", recipe.MealBreakfast, nil, "4 tsp cardamom :: cardamom", []string{"bread"}, nil, 20*time.Minute, 20*time.Minute,
			hands("knead", 20, recipe.StagePrep), wait("rise", 90, recipe.StageRise), hands("shape", 40, ""), wait("rest", 30, recipe.StageRest), wait("bake", 20, recipe.StageBake))},
	}
	return fx
}
