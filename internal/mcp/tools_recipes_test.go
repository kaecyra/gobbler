package mcp_test

import (
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// ADR-00048 obligation 5: get_recipe carries the recipe's author notes, its
// prep, cook and total times, its equipment, meal type and cuisine, so a saved
// recipe can be compared with a preview field for field.
func TestGetRecipeCarriesTheFieldsAPreviewHas(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	ctx := s.as(s.smiths, "ann@example.com")
	pan, err := catalog.New(s.d).CreateEquipment(ctx, catalog.Equipment{Name: "zz mcp loaf tin"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.rec.Create(ctx, recipe.Recipe{
		Name: "Loaf", MealType: recipe.MealBaked, Cuisine: recipe.CuisineFrench,
		Times: recipe.Times{Prep: 20 * time.Minute, Cook: 45 * time.Minute, Total: 75 * time.Minute},
		Components: []recipe.Component{{
			Lines: []recipe.IngredientLine{s.flourLine(3)},
			Steps: []recipe.Step{{ID: "s1", Text: "Mix."}, {ID: "s2", Text: "Bake.", DependsOn: []recipe.StepID{"s1"}}},
		}},
		Equipment:   []recipe.EquipmentLink{{Equipment: recipe.EquipmentID(pan.ID), Qty: quantity.Exact(quantity.Int(2)), Steps: []recipe.StepID{"s2"}}},
		AuthorNotes: []recipe.AuthorNote{{Title: "Storage", Text: "Keeps three days wrapped."}, {Text: "Use strong flour."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := s.personalKey("ann@example.com")
	var got struct {
		MealType     string `json:"meal_type"`
		Cuisine      string `json:"cuisine"`
		PrepMinutes  int    `json:"prep_minutes"`
		CookMinutes  int    `json:"cook_minutes"`
		TotalMinutes int    `json:"total_minutes"`
		Components   []struct {
			Steps []struct {
				ID string `json:"id"`
			} `json:"steps"`
		} `json:"components"`
		Equipment []struct {
			EquipmentID int64    `json:"equipment_id"`
			Name        string   `json:"name"`
			Quantity    string   `json:"quantity"`
			Steps       []string `json:"steps"`
		} `json:"equipment"`
		AuthorNotes []struct {
			Title string `json:"title"`
			Text  string `json:"text"`
		} `json:"author_notes"`
	}
	if msg := call(t, s.connect(secret), "get_recipe", map[string]any{"id": id}, &got); msg != "" {
		t.Fatal(msg)
	}
	t.Run("meal type", func(t *testing.T) {
		if got.MealType != "baked" {
			t.Errorf("meal_type = %q, want baked", got.MealType)
		}
	})
	t.Run("cuisine", func(t *testing.T) {
		if got.Cuisine != "french" {
			t.Errorf("cuisine = %q, want french", got.Cuisine)
		}
	})
	t.Run("times", func(t *testing.T) {
		if got.PrepMinutes != 20 || got.CookMinutes != 45 || got.TotalMinutes != 75 {
			t.Errorf("times = %d/%d/%d minutes, want 20/45/75", got.PrepMinutes, got.CookMinutes, got.TotalMinutes)
		}
	})
	t.Run("equipment", func(t *testing.T) {
		if len(got.Equipment) != 1 || len(got.Components) != 1 || len(got.Components[0].Steps) != 2 {
			t.Fatalf("equipment = %+v, components = %+v", got.Equipment, got.Components)
		}
		e, bake := got.Equipment[0], got.Components[0].Steps[1].ID
		if e.EquipmentID != pan.ID || e.Name != "zz mcp loaf tin" || e.Quantity != "2" || len(e.Steps) != 1 || e.Steps[0] != bake {
			t.Errorf("equipment = %+v, want 2 of the loaf tin (%d) used by step %s", e, pan.ID, bake)
		}
	})
	t.Run("author notes", func(t *testing.T) {
		if len(got.AuthorNotes) != 2 || got.AuthorNotes[0].Title != "Storage" || got.AuthorNotes[0].Text != "Keeps three days wrapped." ||
			got.AuthorNotes[1].Title != "" || got.AuthorNotes[1].Text != "Use strong flour." {
			t.Errorf("author_notes = %+v, want both notes in order", got.AuthorNotes)
		}
	})
}

// A recipe with none of them leaves them out rather than inventing values.
func TestGetRecipeLeavesOutWhatTheRecipeDoesNotHave(t *testing.T) {
	s := newSite(t, config.MCP{Rate: 100, Burst: 100})
	id := s.recipe(s.smiths, "ann@example.com", "Plain", s.flourLine(1))
	secret, _ := s.personalKey("ann@example.com")
	var got map[string]any
	if msg := call(t, s.connect(secret), "get_recipe", map[string]any{"id": id}, &got); msg != "" {
		t.Fatal(msg)
	}
	for _, k := range []string{"meal_type", "cuisine", "prep_minutes", "cook_minutes", "total_minutes", "equipment", "author_notes"} {
		if v, ok := got[k]; ok {
			t.Errorf("%s = %v, want it left out", k, v)
		}
	}
}
