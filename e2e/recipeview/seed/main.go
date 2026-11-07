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
	"image/color"
	"os"
	"time"

	"github.com/kaecyra/gobbler/e2e/recipes/picseed"
	"github.com/kaecyra/gobbler/e2e/seedutil"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/shopping"
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
	// Two recipes have a picture: the first, and the meanwhile recipe, whose
	// object the spec deletes. The clone and the styled recipe have none.
	for _, pid := range []int64{id, meanwhileID} {
		if err := picseed.Give(ctx, d, dir, pid, color.RGBA{R: 60, G: 130, B: 90}); err != nil {
			return err
		}
	}
	cloneID, err := store.New(d).Clone(ctx, id)
	if err != nil {
		return err
	}
	styledID, err := seedStyled(ctx, d)
	if err != nil {
		return err
	}
	doomedID, doomedCloneID, err := seedDoomed(ctx, d, dir)
	if err != nil {
		return err
	}
	listedID, err := seedListed(ctx, d)
	if err != nil {
		return err
	}
	notesID, err := seedNotes(ctx, d)
	if err != nil {
		return err
	}
	ribbonID, err := seedRibbon(ctx, d)
	if err != nil {
		return err
	}
	markdownID, err := seedMarkdown(ctx, d)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int64{"markdown": markdownID, "ribbon": ribbonID, "notes": notesID, "recipe": id, "meanwhile": meanwhileID, "clone": cloneID, "bare_video": bareID, "video_later": laterID, "styled": styledID, "doomed": doomedID, "doomed_clone": doomedCloneID, "listed": listedID})
}

// seedDoomed stores the recipe the delete specs remove: one with a picture, a
// note and a made-it entry, and a clone of it that must survive the delete.
func seedDoomed(ctx context.Context, d *sql.DB, dir string) (id, cloneID int64, err error) {
	rs := store.New(d)
	id, err = rs.Create(ctx, recipe.Recipe{
		Name:       "E2E Doomed Stew",
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "stew", Text: "Stew it."}}}},
	})
	if err != nil {
		return 0, 0, err
	}
	if _, err := rs.AddNote(ctx, id, "Doomed note.", false); err != nil {
		return 0, 0, err
	}
	if _, err := rs.AddMadeIt(ctx, id, store.MadeIt{Comment: "Made the doomed stew."}); err != nil {
		return 0, 0, err
	}
	if err := picseed.Give(ctx, d, dir, id, color.RGBA{R: 140, G: 60, B: 60}); err != nil {
		return 0, 0, err
	}
	cloneID, err = rs.Clone(ctx, id)
	return id, cloneID, err
}

// seedListed stores a recipe that is on a shopping list, which blocks its
// deletion (migration 00005).
func seedListed(ctx context.Context, d *sql.DB) (int64, error) {
	rs := store.New(d)
	id, err := rs.Create(ctx, recipe.Recipe{
		Name:       "E2E Listed Soup",
		Components: []recipe.Component{{Steps: []recipe.Step{{ID: "soup", Text: "Heat the soup."}}}},
	})
	if err != nil {
		return 0, err
	}
	lists := shopping.New(d, rs, catalog.New(d))
	list, err := lists.CreateList(ctx, "E2E delete list")
	if err != nil {
		return 0, err
	}
	if _, err := lists.AddEntry(ctx, list.ID, id, mustRat("1")); err != nil {
		return 0, err
	}
	return id, nil
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
	milk, err := catalog.New(d).CreateIngredient(ctx, catalog.Ingredient{Name: "e2e whole milk", Aisle: "dairy"})
	if err != nil {
		return 0, err
	}
	milkLine := line("1", "cup", milk.ID, quantity.RuleLinear)
	milkLine.Qualifiers = []recipe.Qualifier{{Kind: recipe.QualState, Text: "warmed"}, {Kind: recipe.QualPreparation, Text: "divided"}}
	spice, rating := 2, 4
	r := recipe.Recipe{
		Name:        "E2E Short Ribs",
		Description: "Deeply browned ribs and a slow braise.",
		SourceURL:   "https://www.example.com/short-ribs",
		MealType:    recipe.MealMain,
		Cuisine:     recipe.CuisineKorean,
		SpiceLevel:  &spice,
		Rating:      &rating,
		Servings:    quantity.Exact(mustRat("4")),
		Components: []recipe.Component{{
			Name:  "Ribs",
			Lines: []recipe.IngredientLine{line("2", "lb", ribs.ID, quantity.RuleLinear), milkLine},
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
	if _, err := rs.AddNote(ctx, id, "Salt the ribs the night before.", true); err != nil {
		return 0, err
	}
	if _, err := rs.AddNote(ctx, id, "Private remark, not pinned.", false); err != nil {
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

// seedRibbon stores a recipe with enough alternating steps for its large ribbon
// to need merging on a phone: hands-on and waiting steps take turns. The ribbon
// adds up time by stage kind, so the two rises are one segment (five in all),
// and the last two waiting kinds merge into one segment at the merged level.
func seedRibbon(ctx context.Context, d *sql.DB) (int64, error) {
	step := func(id string, minutes int, passive bool, kind recipe.StageKind) recipe.Step {
		return recipe.Step{ID: recipe.StepID(id), Text: "Do " + id + ".", Duration: time.Duration(minutes) * time.Minute, Passive: passive, Kind: kind}
	}
	return store.New(d).Create(ctx, recipe.Recipe{
		Name: "E2E Cardamom Knots",
		Components: []recipe.Component{{
			Name: "Main",
			Steps: []recipe.Step{
				step("knead", 20, false, recipe.StagePrep),
				step("rise", 90, true, recipe.StageRise),
				step("shape", 40, false, ""),
				step("rest", 30, true, recipe.StageRest),
				step("proof", 45, true, recipe.StageRise),
				step("bake", 20, true, recipe.StageBake),
			},
		}},
	})
}

// seedNotes stores the recipe whose page ends in the author's notes
// (ADR-00041): two parts of steps, then three notes, the first two titled.
func seedNotes(ctx context.Context, d *sql.DB) (int64, error) {
	return store.New(d).Create(ctx, recipe.Recipe{
		Name: "E2E Braised Ribs",
		Components: []recipe.Component{
			{Name: "Braise", Steps: []recipe.Step{
				{ID: "sear", Text: "Sear the ribs on all sides.", Duration: 15 * time.Minute},
				{ID: "braise", Text: "Braise in the oven.", Duration: 3 * time.Hour, Passive: true, DependsOn: []recipe.StepID{"sear"}},
			}},
			{Name: "Finish", Steps: []recipe.Step{
				{ID: "serve", Text: "Reduce the sauce and serve over mash.", Duration: 15 * time.Minute, DependsOn: []recipe.StepID{"braise"}},
			}},
		},
		AuthorNotes: []recipe.AuthorNote{
			{Title: "Wine", Text: "Any dry red you would drink works. A cooking wine adds salt you then have to cook out."},
			{Title: "Make ahead", Text: "The braise is better the next day. Cool the ribs in their liquid, lift off the set fat, and reheat gently."},
			{Text: "Bone-in ribs give a richer sauce than boneless, but boneless cook about 30 minutes faster."},
		},
	})
}

// seedMarkdown stores a recipe whose step and author note are written in
// markdown (ADR-00046): bold and lists to render, and a script tag and a
// heading that must stay text.
func seedMarkdown(ctx context.Context, d *sql.DB) (int64, error) {
	return store.New(d).Create(ctx, recipe.Recipe{
		Name: "E2E Marked Custard",
		Components: []recipe.Component{{
			Name: "Main",
			Steps: []recipe.Step{
				{ID: "whisk", Text: "Whisk **hard** until pale.\n- 3 eggs\n- 100 g sugar\n\n<script>window.injected = 1</script>\n# Not a heading", Duration: 5 * time.Minute},
				{ID: "bake", Text: "Bake in a water bath:\n1. Fill the tray\n2. Bake 40 minutes", Duration: 40 * time.Minute, Passive: true, DependsOn: []recipe.StepID{"whisk"}},
			},
		}},
		AuthorNotes: []recipe.AuthorNote{
			{Title: "Swaps", Text: "Use **cream** or:\n- whole milk\n- oat milk"},
		},
	})
}
