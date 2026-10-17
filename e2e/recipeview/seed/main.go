// Command seed fills a fresh data directory with the recipe the recipe-view
// e2e spec reads, a second one with meanwhile guidance and a clone of the first,
// and two recipes with a YouTube video (one with a stored thumbnail, one
// without), then prints their ids as JSON. It is test tooling (ADR-00013):
// it goes through the real store and catalog packages, so the spec exercises
// the same read path as production. Usage: seed <data-dir>.
package main

import (
	"context"
	"database/sql"
	"encoding/base64"
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

// The video IDs the recipe-view spec expects (11 characters each).
const (
	VideoWithThumbnail    = "dQw4w9WgXcQ"
	VideoWithoutThumbnail = "9bZkp7q19f0"
)

// onePixelPNG is a 1x1 PNG, the stored thumbnail.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

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
	density := 0.5
	flour, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e flour", Aisle: "baking", DensityGPerML: &density})
	if err != nil {
		return err
	}
	sugar, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e sugar", Aisle: "baking"})
	if err != nil {
		return err
	}
	egg, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e egg", Aisle: "dairy"})
	if err != nil {
		return err
	}
	bay, err := cat.CreateIngredient(ctx, catalog.Ingredient{Name: "e2e bay leaf", Aisle: "spices"})
	if err != nil {
		return err
	}
	pan, err := cat.CreateEquipment(ctx, catalog.Equipment{Name: "e2e 9x13 pan", Sized: true})
	if err != nil {
		return err
	}
	whisk, err := cat.CreateEquipment(ctx, catalog.Equipment{Name: "e2e whisk"})
	if err != nil {
		return err
	}

	r := recipe.Recipe{
		Name:     "E2E Sponge",
		Servings: quantity.Exact(mustRat("4")),
		Yield:    recipe.Yield{Qty: quantity.Exact(mustRat("1")), Unit: "cake"},
		Times:    recipe.Times{Prep: 20 * time.Minute, Cook: 45 * time.Minute},
		Components: []recipe.Component{{
			Name: "Batter",
			Lines: []recipe.IngredientLine{
				line("1 1/3", "cup", flour.ID, quantity.RuleLinear),
				line("1/3", "cup", sugar.ID, quantity.RuleLinear),
				line("2", "", egg.ID, quantity.RuleLinear),
				line("1", "", bay.ID, quantity.RuleFixed),
			},
			Steps: []recipe.Step{
				{ID: "mix", Text: "Mix everything.", Duration: 10 * time.Minute},
				{ID: "bake", Text: "Bake until golden.", Duration: 45 * time.Minute, Passive: true, DependsOn: []recipe.StepID{"mix"}},
			},
		}},
		Equipment: []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(pan.ID)}, {Equipment: recipe.EquipmentID(whisk.ID)}},
	}
	// The first recipe carries a video whose thumbnail is stored, so the page
	// shows the thumbnail without the server ever reaching YouTube.
	r.VideoID = VideoWithThumbnail
	id, err := store.New(d).Create(ctx, r)
	if err != nil {
		return err
	}
	png, err := base64.StdEncoding.DecodeString(onePixelPNG)
	if err != nil {
		return err
	}
	if err := store.New(d).SaveThumbnail(ctx, VideoWithThumbnail, png, "image/png"); err != nil {
		return err
	}
	// The second has a video and no thumbnail, as after a failed download.
	bare := recipe.Recipe{
		Name: "E2E Video Placeholder", VideoID: VideoWithoutThumbnail,
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "watch", Text: "Watch the video."}}}},
	}
	bareID, err := store.New(d).Create(ctx, bare)
	if err != nil {
		return err
	}

	// The third has no video yet: the spec adds one through the editor, and the
	// save downloads its thumbnail from the fixture host.
	plain := recipe.Recipe{
		Name:       "E2E Video Later",
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "wait", Text: "Add a video later."}}}},
	}
	laterID, err := store.New(d).Create(ctx, plain)
	if err != nil {
		return err
	}

	// The meanwhile recipe: while the dough rises, the sauce can be made.
	pizza := recipe.Recipe{
		Name: "E2E Pizza",
		Components: []recipe.Component{
			{Name: "Dough", Steps: []recipe.Step{
				{ID: "mix", Text: "Mix the dough.", Duration: 10 * time.Minute},
				{ID: "rise", Text: "Let the dough rise.", Duration: time.Hour, Passive: true, DependsOn: []recipe.StepID{"mix"}},
				{ID: "bake", Text: "Bake the pizza.", Duration: 15 * time.Minute, DependsOn: []recipe.StepID{"rise"}},
			}},
			{Name: "Sauce", Steps: []recipe.Step{
				{ID: "chop", Text: "Chop the tomatoes.", Duration: 5 * time.Minute},
				{ID: "simmer", Text: "Simmer the sauce.", Duration: 20 * time.Minute, DependsOn: []recipe.StepID{"chop"}},
			}},
			{Name: "Finish", Steps: []recipe.Step{
				{ID: "serve", Text: "Top and serve.", Duration: time.Minute, DependsOn: []recipe.StepID{"bake", "simmer"}},
			}},
		},
	}
	meanwhileID, err := store.New(d).Create(ctx, pizza)
	if err != nil {
		return err
	}
	cloneID, err := store.New(d).Clone(ctx, id)
	if err != nil {
		return err
	}
	styledID, err := seedStyled(ctx, d)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"recipe": id, "meanwhile": meanwhileID, "clone": cloneID, "bare_video": bareID, "video_later": laterID, "styled": styledID})
}

// seedStyled stores a recipe that uses every field the mockup design shows: a
// meal type, a spice level, a rating, a source link, step titles and stage
// kinds, a pinned and an unpinned note, and a made-it log.
func seedStyled(ctx context.Context, d *sql.DB) (int64, error) {
	rs := store.New(d)
	ribs, err := catalog.New(d).CreateIngredient(ctx, catalog.Ingredient{Name: "e2e short ribs", Aisle: "meat"})
	if err != nil {
		return 0, err
	}
	spice, rating := 2, 4
	r := recipe.Recipe{
		Name:        "E2E Short Ribs",
		Description: "Deeply browned ribs and a slow braise.",
		SourceURL:   "https://www.example.com/short-ribs",
		MealType:    recipe.MealMain,
		SpiceLevel:  &spice,
		Rating:      &rating,
		Servings:    quantity.Exact(mustRat("4")),
		Components: []recipe.Component{{
			Name: "Ribs",
			Lines: []recipe.IngredientLine{line("2", "lb", ribs.ID, quantity.RuleLinear)},
			Steps: []recipe.Step{
				{ID: "brown", Title: "Brown the ribs", Text: "Brown the ribs in two batches.", Duration: 20 * time.Minute, Kind: recipe.StagePrep},
				{ID: "braise", Title: "Braise", Text: "Cover and braise until tender.", Duration: 3 * time.Hour, Passive: true, Kind: recipe.StageBraise, DependsOn: []recipe.StepID{"brown"}},
			},
		}},
	}
	id, err := rs.Create(ctx, r)
	if err != nil {
		return 0, err
	}
	tip, err := rs.AddNote(ctx, id, "Salt the ribs the night before.")
	if err != nil {
		return 0, err
	}
	if err := rs.SetNotePinned(ctx, id, tip.ID, true); err != nil {
		return 0, err
	}
	if _, err := rs.AddNote(ctx, id, "Private remark, not pinned."); err != nil {
		return 0, err
	}
	for i := 0; i < 2; i++ {
		if _, err := rs.AddMadeIt(ctx, id, store.MadeIt{Comment: "Made it."}); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func line(qty, unit string, ingredient int64, rule quantity.Rule) recipe.IngredientLine {
	a := quantity.Amount{Qty: quantity.Exact(mustRat(qty))}
	if unit != "" {
		a.Unit = quantity.MustUnit(unit)
	}
	return recipe.IngredientLine{Amount: a, Ingredient: recipe.IngredientID(ingredient), Rule: rule}
}

func mustRat(s string) quantity.Rat {
	r, err := quantity.ParseRat(s)
	if err != nil {
		panic(err)
	}
	return r
}
