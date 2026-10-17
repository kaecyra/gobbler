package ingest

import (
	"strings"
	"testing"
)

const groupedPage = `<!doctype html><html><head><title>Filled Buns</title>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"Recipe","name":"Filled Buns",
 "recipeIngredient":["3 cups flour","1 lb pork, minced","2 tbsp soy sauce"],
 "recipeInstructions":[{"@type":"HowToStep","text":"Mix the dough."},{"@type":"HowToStep","text":"Fill and steam."}]}
</script></head><body>
<div class="wprm-recipe-container">
 <div class="wprm-recipe-ingredient-group"><h4 class="wprm-recipe-group-name">For the dough</h4>
  <ul><li class="wprm-recipe-ingredient">3 cups flour</li></ul></div>
 <div class="wprm-recipe-ingredient-group"><h4 class="wprm-recipe-group-name">For the filling</h4>
  <ul><li class="wprm-recipe-ingredient">1 lb pork minced</li><li class="wprm-recipe-ingredient">2 tbsp soy sauce</li></ul></div>
 <div class="wprm-recipe-instruction-group"><h4 class="wprm-recipe-group-name">Dough</h4>
  <ul><li class="wprm-recipe-instruction">Mix the dough.</li></ul></div>
 <div class="wprm-recipe-instruction-group"><h4 class="wprm-recipe-group-name">Finish</h4>
  <ul><li class="wprm-recipe-instruction">Fill and steam.</li></ul></div>
</div></body></html>`

// A recipe plugin's ingredient groups become the draft's components, with the
// steps of the same-named section in the matching component.
func TestFromURLGroupsIngredientsByPluginMarkup(t *testing.T) {
	e := newEnv(t)
	srv, _ := serve(t, []byte(groupedPage))
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/buns")
	if err != nil {
		t.Fatalf("FromURL: %v", err)
	}
	type shape struct {
		name         string
		lines, steps int
	}
	var got []shape
	for _, c := range d.Components {
		got = append(got, shape{c.Name, len(c.Lines), len(c.Steps)})
	}
	want := []shape{{"For the dough", 1, 0}, {"For the filling", 2, 0}, {"Dough", 0, 1}, {"Finish", 0, 1}}
	if len(got) != len(want) {
		t.Fatalf("components = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("component %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if raw := d.Components[1].Lines[0].Raw; raw != "1 lb pork, minced" {
		t.Errorf("line raw = %q, want the JSON-LD string", raw)
	}
}

// A step section named like an ingredient group shares its component, so the
// lines and steps of one part of the recipe sit together.
func TestFromURLStepSectionJoinsSameNamedIngredientGroup(t *testing.T) {
	e := newEnv(t)
	page := strings.Replace(groupedPage, "For the dough", "Dough", 1)
	srv, _ := serve(t, []byte(page))
	d, err := e.pipeline(t, srv.Client()).FromURL(e.ctx, srv.URL+"/buns")
	if err != nil {
		t.Fatalf("FromURL: %v", err)
	}
	if len(d.Components) != 3 {
		t.Fatalf("components = %+v, want Dough, For the filling, Finish", d.Components)
	}
	if c := d.Components[0]; c.Name != "Dough" || len(c.Lines) != 1 || len(c.Steps) != 1 {
		t.Errorf("first component = %q with %d lines, %d steps, want Dough with 1 and 1", c.Name, len(c.Lines), len(c.Steps))
	}
}
