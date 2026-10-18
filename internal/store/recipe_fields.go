package store

import (
	"context"
	"fmt"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// The optional fields ADR-00022 added to the recipe document: meal type and
// spice level on the recipe, stage kind and title on a step. Like the video ID
// they are written by statements of their own (queries/recipe_fields.sql). The
// model has already checked the closed sets; the columns' CHECK constraints
// refuse anything that got past it.

// setRecipeFields writes the meal type, spice level and cuisine of recipe id inside the
// transaction q is bound to.
func setRecipeFields(ctx context.Context, q *storedb.Queries, hid, id int64, r recipe.Recipe) error {
	err := q.SetRecipeFields(ctx, storedb.SetRecipeFieldsParams{
		MealType: string(r.MealType), SpiceLevel: nullInt(r.SpiceLevel), ID: id, HouseholdID: hid,
	})
	if err != nil {
		return wrapWrite(fmt.Sprintf("write recipe %d meal type and spice level", id), err)
	}
	return setRecipeCuisine(ctx, q, hid, id, r.Cuisine)
}

// getRecipeFields reads the meal type and spice level of recipe id.
func getRecipeFields(ctx context.Context, q *storedb.Queries, hid, id int64) (recipe.MealType, *int, error) {
	row, err := q.GetRecipeFields(ctx, storedb.GetRecipeFieldsParams{ID: id, HouseholdID: hid})
	if err != nil {
		return "", nil, fmt.Errorf("read recipe %d meal type and spice level: %w", id, err)
	}
	return recipe.MealType(row.MealType), intPtr(row.SpiceLevel), nil
}

// setRecipeCuisine writes the cuisine of recipe id (ADR-00025).
func setRecipeCuisine(ctx context.Context, q *storedb.Queries, hid, id int64, c recipe.Cuisine) error {
	if err := q.SetRecipeCuisine(ctx, storedb.SetRecipeCuisineParams{Cuisine: string(c), ID: id, HouseholdID: hid}); err != nil {
		return wrapWrite(fmt.Sprintf("write recipe %d cuisine", id), err)
	}
	return nil
}

// getRecipeCuisine reads the cuisine of recipe id.
func getRecipeCuisine(ctx context.Context, q *storedb.Queries, hid, id int64) (recipe.Cuisine, error) {
	c, err := q.GetRecipeCuisine(ctx, storedb.GetRecipeCuisineParams{ID: id, HouseholdID: hid})
	if err != nil {
		return "", fmt.Errorf("read recipe %d cuisine: %w", id, err)
	}
	return recipe.Cuisine(c), nil
}

// setStepFields writes the stage kind and title of the step row stepRow.
func setStepFields(ctx context.Context, q *storedb.Queries, hid, stepRow int64, s recipe.Step) error {
	err := q.SetStepFields(ctx, storedb.SetStepFieldsParams{StageKind: string(s.Kind), Title: s.Title, ID: stepRow, HouseholdID: hid})
	if err != nil {
		return wrapWrite(fmt.Sprintf("write step %s stage kind and title", s.ID), err)
	}
	return nil
}

// stepFields is the stage kind and title of one stored step.
type stepFields struct {
	kind  recipe.StageKind
	title string
}

// loadStepFields reads the stage kind and title of every step of recipe id,
// keyed by step row.
func loadStepFields(ctx context.Context, q *storedb.Queries, hid, id int64) (map[int64]stepFields, error) {
	rows, err := q.ListStepFields(ctx, storedb.ListStepFieldsParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("step stage kinds and titles", err)
	}
	out := make(map[int64]stepFields, len(rows))
	for _, r := range rows {
		out[r.ID] = stepFields{kind: recipe.StageKind(r.StageKind), title: r.Title}
	}
	return out, nil
}
