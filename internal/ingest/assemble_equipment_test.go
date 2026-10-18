package ingest

import (
	"reflect"
	"testing"

	"github.com/kaecyra/gobbler/internal/catalog"
)

const toolsPage = `<!doctype html><html><head><title>Braise</title>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Braise",
 "recipeIngredient":["1 onion","2 tbsp butter"],
 "tool":["Dutch oven",{"@type":"HowToTool","name":"Flux capacitor (optional)"},"dutch ovens"],
 "recipeInstructions":[{"@type":"HowToStep","text":"Slice the onion."},{"@type":"HowToStep","text":"Brown it in the dutch oven, then bake."}]}
</script></head><body></body></html>`

// equipmentOf summarises a draft's equipment as name, with a marker for the
// ones the catalog lacks.
func equipmentOf(d Draft) []string {
	var out []string
	for _, e := range d.Equipment {
		if e.Equipment == 0 {
			out = append(out, e.Name+"?")
			continue
		}
		out = append(out, e.Name)
	}
	return out
}

func TestFromURLReadsSchemaToolsAndMatchesTheCatalog(t *testing.T) {
	e := newEnv(t)
	srv, _ := serve(t, []byte(toolsPage))
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/braise")
	if err != nil {
		t.Fatalf("FromURL: %v", err)
	}
	// "Dutch oven" matches the catalog (and its plural and the step text name
	// the same one, so it is listed once); the flux capacitor is unknown, with its
	// qualifier dropped, and is proposed.
	want := []string{"dutch oven", "flux capacitor?"}
	if got := equipmentOf(d); !reflect.DeepEqual(got, want) {
		t.Fatalf("equipment = %q, want %q", got, want)
	}
	if got := d.ProposedEquipment(); !reflect.DeepEqual(got, []ProposedEquipment{{Name: "flux capacitor"}}) {
		t.Errorf("proposed = %+v", got)
	}
	for _, eq := range d.Equipment {
		if eq.FromLLM {
			t.Errorf("%q is marked FromLLM on the rule path", eq.Name)
		}
	}
}

func TestFromTextFindsCatalogEquipmentInStepAndIngredientText(t *testing.T) {
	e := newEnv(t)
	text := "Sheet Pan Dinner\n\nIngredients\n1 lb potatoes\n1 parchment paper sheet\n2 tbsp oil\n\n" +
		"Directions\n1. Heat the oven to 200C.\n2. Spread on a sheet pan and roast; stir with a whisk. Potato spans the pan.\n3. Cool on wire racks.\n"
	d, err := e.pipeline(t, nil).FromText(e.ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	// Reading order: ingredient lines first, then steps; whole words and
	// plurals only ("spans", "potato" do not match "pan", "pot").
	want := []string{"parchment paper", "oven", "baking sheet", "whisk", "wire rack"}
	if got := equipmentOf(d); !reflect.DeepEqual(got, want) {
		t.Errorf("equipment = %q, want %q", got, want)
	}
	if got := d.ProposedEquipment(); len(got) != 0 {
		t.Errorf("text matching proposes nothing, got %+v", got)
	}
}

func TestEquipmentPrefersTheLongerName(t *testing.T) {
	e := newEnv(t)
	if _, err := e.catalog.CreateEquipment(e.ctx, catalog.Equipment{Name: "oven mitt", Reviewed: true}); err != nil {
		t.Fatal(err)
	}
	text := "Dough\n\nIngredients\n2 cups flour\n\nDirections\n1. Knead in the stand mixer.\n2. Lift it with an oven mitt, not bare hands.\n"
	d, err := e.pipeline(t, nil).FromText(e.ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	// "oven mitt" starts where "oven" does; the longer name wins.
	want := []string{"stand mixer", "oven mitt"}
	if got := equipmentOf(d); !reflect.DeepEqual(got, want) {
		t.Errorf("equipment = %q, want %q", got, want)
	}
}

func TestEquipmentDraftIsDeterministic(t *testing.T) {
	e := newEnv(t)
	srv, _ := serve(t, []byte(toolsPage))
	p := e.pipeline(t, srv.Client())
	a, err := p.FromURL(e.ctx, srv.URL+"/braise")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.FromURL(e.ctx, srv.URL+"/braise")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Equipment, b.Equipment) {
		t.Errorf("same input gave %+v then %+v", a.Equipment, b.Equipment)
	}
}

func TestEquipmentNeverCreatesCatalogEntriesWhileDrafting(t *testing.T) {
	e := newEnv(t)
	srv, _ := serve(t, []byte(toolsPage))
	if _, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/braise"); err != nil {
		t.Fatal(err)
	}
	unrev, err := e.catalog.ListUnreviewedEquipment(e.ctx)
	if err != nil || len(unrev) != 0 {
		t.Errorf("after ingest: unreviewed equipment %+v, err %v (ADR-00008 obligation 3)", unrev, err)
	}
}

func TestParserVersionCoversEquipment(t *testing.T) {
	// Equipment changes what a draft holds, so drafts from older rules must be
	// distinguishable (ParserVersion's contract).
	if ParserVersion == "2" {
		t.Errorf("ParserVersion %q predates equipment extraction", ParserVersion)
	}
}

// "Mandoline slicer" and "pastry blender" are what recipe pages call catalog
// items; migration 00017 aliases them, so ingestion matches rather than
// proposing duplicates.
func TestFromTextMatchesMandolineSlicerAndPastryBlenderToTheCatalog(t *testing.T) {
	e := newEnv(t)
	text := "Biscuits\n\nIngredients\n2 cups flour\n\nDirections\n1. Cut the butter in with a pastry blender.\n2. Slice the apples on a mandoline slicer.\n"
	d, err := e.pipeline(t, nil).FromText(e.ctx, text)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pastry cutter", "mandoline"}
	if got := equipmentOf(d); !reflect.DeepEqual(got, want) {
		t.Errorf("equipment = %q, want %q", got, want)
	}
	if got := d.ProposedEquipment(); len(got) != 0 {
		t.Errorf("proposed = %+v, want none", got)
	}
}

// A page's own tool list is matched by name, not by words in the steps, so
// this is the path that needs the aliases themselves.
func TestFromURLMatchesToolListAliasesToTheCatalog(t *testing.T) {
	e := newEnv(t)
	page := `<!doctype html><html><head><title>Biscuits</title>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Biscuits",
 "recipeIngredient":["2 cups flour"],
 "tool":["Mandoline slicer","Pastry blender"],
 "recipeInstructions":[{"@type":"HowToStep","text":"Mix."}]}
</script></head><body></body></html>`
	srv, _ := serve(t, []byte(page))
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/biscuits")
	if err != nil {
		t.Fatalf("FromURL: %v", err)
	}
	want := []string{"mandoline", "pastry cutter"}
	if got := equipmentOf(d); !reflect.DeepEqual(got, want) {
		t.Errorf("equipment = %q, want %q", got, want)
	}
	if got := d.ProposedEquipment(); len(got) != 0 {
		t.Errorf("proposed = %+v, want none", got)
	}
}
