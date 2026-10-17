package extract

import (
	"reflect"
	"testing"
)

func TestToolsFromJSONLD(t *testing.T) {
	page := `<html><head><script type="application/ld+json">{
		"@type": "Recipe", "name": "Stew",
		"recipeIngredient": ["1 onion"],
		"tool": ["Dutch oven", {"@type": "HowToTool", "name": "Whisk"}, {"@type": "HowToTool", "name": "  "}]
	}</script></head><body></body></html>`
	res := mustExtract(t, page, fixtureURL)
	if res.Recipe == nil {
		t.Fatalf("no recipe: %+v", res)
	}
	want := []string{"Dutch oven", "Whisk"}
	if !reflect.DeepEqual(res.Recipe.Tools, want) {
		t.Errorf("Tools = %q, want %q", res.Recipe.Tools, want)
	}
}

func TestToolsFromJSONLDSingleObject(t *testing.T) {
	page := `<html><head><script type="application/ld+json">{
		"@type": "Recipe", "name": "Stew", "recipeIngredient": ["1 onion"],
		"tool": {"@type": "HowToTool", "name": "Skillet"}
	}</script></head><body></body></html>`
	res := mustExtract(t, page, fixtureURL)
	if res.Recipe == nil || !reflect.DeepEqual(res.Recipe.Tools, []string{"Skillet"}) {
		t.Errorf("Tools = %+v", res.Recipe)
	}
}

func TestToolsFromMicrodata(t *testing.T) {
	page := `<html><body><div itemscope itemtype="https://schema.org/Recipe">
		<h1 itemprop="name">Stew</h1>
		<span itemprop="recipeIngredient">1 onion</span>
		<span itemprop="tool">Stockpot</span>
		<div itemprop="tool" itemscope itemtype="https://schema.org/HowToTool"><span itemprop="name">Ladle</span></div>
	</div></body></html>`
	res := mustExtract(t, page, fixtureURL)
	if res.Method != MethodMicrodata || res.Recipe == nil {
		t.Fatalf("result = %+v", res)
	}
	want := []string{"Stockpot", "Ladle"}
	if !reflect.DeepEqual(res.Recipe.Tools, want) {
		t.Errorf("Tools = %q, want %q", res.Recipe.Tools, want)
	}
}
