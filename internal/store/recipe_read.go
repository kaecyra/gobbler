package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// loadDocument reads the recipe document with the given id. Empty collections
// come back nil. A missing recipe is ErrNotFound.
func loadDocument(ctx context.Context, q *storedb.Queries, hid, id int64, units *unitTable) (recipe.Recipe, error) {
	row, err := q.GetRecipeDocument(ctx, storedb.GetRecipeDocumentParams{ID: id, HouseholdID: hid})
	if errors.Is(err, sql.ErrNoRows) {
		return recipe.Recipe{}, fmt.Errorf("%w: recipe %d", ErrNotFound, id)
	}
	if err != nil {
		return recipe.Recipe{}, fmt.Errorf("read recipe %d: %w", id, err)
	}
	r := recipe.Recipe{Name: row.Name, Description: row.Description, SourceURL: row.SourceUrl, Author: row.Author}
	r.Yield.Unit = row.YieldUnit
	if r.Yield.Qty, err = decodeQty(stringPtr(row.YieldQty)); err != nil {
		return recipe.Recipe{}, fmt.Errorf("recipe %d yield: %w", id, err)
	}
	if r.Servings, err = decodeQty(stringPtr(row.Servings)); err != nil {
		return recipe.Recipe{}, fmt.Errorf("recipe %d servings: %w", id, err)
	}
	r.CaloriesPerServing, r.Rating = intPtr(row.CaloriesPerServing), intPtr(row.Rating)
	r.Times = recipe.Times{Prep: time.Duration(row.PrepNs), Cook: time.Duration(row.CookNs), Total: time.Duration(row.TotalNs)}

	if r.Tags, err = loadTags(ctx, q, hid, id); err != nil {
		return recipe.Recipe{}, err
	}
	if r.Times.Special, err = loadSpecialTimes(ctx, q, hid, id); err != nil {
		return recipe.Recipe{}, err
	}
	if r.Components, err = loadComponents(ctx, q, hid, id, units); err != nil {
		return recipe.Recipe{}, err
	}
	if r.Equipment, err = loadEquipment(ctx, q, hid, id); err != nil {
		return recipe.Recipe{}, err
	}
	return r, nil
}

func loadTags(ctx context.Context, q *storedb.Queries, hid, id int64) ([]string, error) {
	tags, err := q.ListRecipeTags(ctx, storedb.ListRecipeTagsParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("tags", err)
	}
	return tags, nil
}

func loadSpecialTimes(ctx context.Context, q *storedb.Queries, hid, id int64) ([]recipe.SpecialTime, error) {
	rows, err := q.ListSpecialTimes(ctx, storedb.ListSpecialTimesParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("special times", err)
	}
	var out []recipe.SpecialTime
	for _, r := range rows {
		out = append(out, recipe.SpecialTime{Label: r.Label, Duration: time.Duration(r.DurationNs)})
	}
	return out, nil
}

func loadComponents(ctx context.Context, q *storedb.Queries, hid, id int64, units *unitTable) ([]recipe.Component, error) {
	compRows, err := q.ListComponents(ctx, storedb.ListComponentsParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("components", err)
	}
	var comps []recipe.Component
	compIdx := map[int64]int{}
	for _, r := range compRows {
		compIdx[r.ID] = len(comps)
		comps = append(comps, recipe.Component{Name: r.Name})
	}

	lineRows, err := q.ListIngredientLines(ctx, storedb.ListIngredientLinesParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("ingredient lines", err)
	}
	lineIdx := map[int64][2]int{} // line id -> component index, line index
	for _, r := range lineRows {
		l, err := lineFrom(r, units)
		if err != nil {
			return nil, readErr("ingredient lines", err)
		}
		ci, err := lookup(compIdx, r.ComponentID, "component")
		if err != nil {
			return nil, readErr("ingredient lines", err)
		}
		lineIdx[r.ID] = [2]int{ci, len(comps[ci].Lines)}
		comps[ci].Lines = append(comps[ci].Lines, l)
	}

	qualRows, err := q.ListLineQualifiers(ctx, storedb.ListLineQualifiersParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("line qualifiers", err)
	}
	for _, r := range qualRows {
		at, err := lookup(lineIdx, r.LineID, "ingredient line")
		if err != nil {
			return nil, readErr("line qualifiers", err)
		}
		line := &comps[at[0]].Lines[at[1]]
		line.Qualifiers = append(line.Qualifiers, recipe.Qualifier{Kind: recipe.QualifierKind(r.Kind), Text: r.Text})
	}

	stepRows, err := q.ListSteps(ctx, storedb.ListStepsParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("steps", err)
	}
	stepKeys := map[int64]recipe.StepID{}
	stepIdx := map[int64][2]int{}
	for _, r := range stepRows {
		s := recipe.Step{ID: recipe.StepID(r.StepKey), Text: r.Text, Duration: time.Duration(r.DurationNs), Passive: r.Passive == 1}
		ci, err := lookup(compIdx, r.ComponentID, "component")
		if err != nil {
			return nil, readErr("steps", err)
		}
		stepKeys[r.ID] = s.ID
		stepIdx[r.ID] = [2]int{ci, len(comps[ci].Steps)}
		comps[ci].Steps = append(comps[ci].Steps, s)
	}

	depRows, err := q.ListStepDependencies(ctx, storedb.ListStepDependenciesParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("step dependencies", err)
	}
	for _, r := range depRows {
		at, err := lookup(stepIdx, r.StepID, "step")
		if err != nil {
			return nil, readErr("step dependencies", err)
		}
		depKey, err := lookup(stepKeys, r.DependsOnStepID, "step")
		if err != nil {
			return nil, readErr("step dependencies", fmt.Errorf("dependency of step %s: %w", stepKeys[r.StepID], err))
		}
		step := &comps[at[0]].Steps[at[1]]
		step.DependsOn = append(step.DependsOn, depKey)
	}
	return comps, nil
}

// lineFrom converts a stored ingredient line row, without its qualifiers.
func lineFrom(r storedb.ListIngredientLinesRow, units *unitTable) (recipe.IngredientLine, error) {
	l := recipe.IngredientLine{
		Ingredient: recipe.IngredientID(r.IngredientID.Int64), Optional: r.Optional == 1,
		Rule: quantity.Rule(r.Rule), Raw: r.Raw,
	}
	var err error
	if l.Amount.Qty, err = decodeQty(stringPtr(r.Qty)); err != nil {
		return recipe.IngredientLine{}, err
	}
	if l.Amount.Unit, err = units.unit(r.UnitID); err != nil {
		return recipe.IngredientLine{}, err
	}
	if r.HasPackage == 1 {
		l.Amount.HasPackage = true
		if l.Amount.Package.Qty, err = decodeQty(stringPtr(r.PackageQty)); err != nil {
			return recipe.IngredientLine{}, err
		}
		if l.Amount.Package.Unit, err = units.unit(r.PackageUnitID); err != nil {
			return recipe.IngredientLine{}, err
		}
	}
	return l, nil
}

func loadEquipment(ctx context.Context, q *storedb.Queries, hid, id int64) ([]recipe.EquipmentLink, error) {
	eqRows, err := q.ListRecipeEquipment(ctx, storedb.ListRecipeEquipmentParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("equipment", err)
	}
	var out []recipe.EquipmentLink
	rowIdx := map[int64]int{}
	for _, r := range eqRows {
		qty, err := decodeQty(stringPtr(r.Qty))
		if err != nil {
			return nil, readErr("equipment", err)
		}
		rowIdx[r.ID] = len(out)
		out = append(out, recipe.EquipmentLink{Equipment: recipe.EquipmentID(r.EquipmentID), Qty: qty})
	}
	stepRows, err := q.ListEquipmentSteps(ctx, storedb.ListEquipmentStepsParams{RecipeID: id, HouseholdID: hid})
	if err != nil {
		return nil, readErr("equipment steps", err)
	}
	for _, r := range stepRows {
		at, err := lookup(rowIdx, r.RecipeEquipmentID, "equipment link")
		if err != nil {
			return nil, readErr("equipment steps", err)
		}
		l := &out[at]
		l.Steps = append(l.Steps, recipe.StepID(r.StepKey))
	}
	return out, nil
}
