package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/recipe"
	"github.com/kaecyra/gobbler/internal/store/storedb"
)

// documentColumns are the scalar columns of a recipes row that come from the
// document. An insert and an update bind the same twelve, so both queries are
// filled from this one conversion.
func documentColumns(r recipe.Recipe) (storedb.UpdateRecipeDocumentParams, error) {
	yq, err := encodeQty(r.Yield.Qty)
	if err != nil {
		return storedb.UpdateRecipeDocumentParams{}, fmt.Errorf("yield: %w", err)
	}
	sv, err := encodeQty(r.Servings)
	if err != nil {
		return storedb.UpdateRecipeDocumentParams{}, fmt.Errorf("servings: %w", err)
	}
	return storedb.UpdateRecipeDocumentParams{
		Name: r.Name, Description: r.Description, SourceUrl: r.SourceURL, Author: r.Author,
		YieldQty: nullString(yq), YieldUnit: r.Yield.Unit, Servings: nullString(sv),
		CaloriesPerServing: nullInt(r.CaloriesPerServing), Rating: nullInt(r.Rating),
		PrepNs: int64(r.Times.Prep), CookNs: int64(r.Times.Cook), TotalNs: int64(r.Times.Total),
	}, nil
}

// insertChildren writes everything below the recipes row: tags, special times,
// components with their lines and steps, step dependencies and equipment. The
// row must exist and own no children.
func insertChildren(ctx context.Context, q *storedb.Queries, id int64, r recipe.Recipe, units *unitTable) error {
	for i, tag := range r.Tags {
		if err := q.InsertRecipeTag(ctx, storedb.InsertRecipeTagParams{RecipeID: id, Position: int64(i), Tag: tag}); err != nil {
			return wrapWrite(fmt.Sprintf("write tag %q", tag), err)
		}
	}
	for i, st := range r.Times.Special {
		err := q.InsertSpecialTime(ctx, storedb.InsertSpecialTimeParams{
			RecipeID: id, Position: int64(i), Label: st.Label, DurationNs: int64(st.Duration),
		})
		if err != nil {
			return wrapWrite(fmt.Sprintf("write special time %q", st.Label), err)
		}
	}

	stepRows := make(map[recipe.StepID]int64)
	for ci, c := range r.Components {
		compID, err := q.InsertComponent(ctx, storedb.InsertComponentParams{RecipeID: id, Position: int64(ci), Name: c.Name})
		if err != nil {
			return wrapWrite(fmt.Sprintf("write component %d", ci+1), err)
		}
		for li, l := range c.Lines {
			if err := insertLine(ctx, q, compID, li, l, units); err != nil {
				return fmt.Errorf("component %d line %d: %w", ci+1, li+1, err)
			}
		}
		for si, s := range c.Steps {
			stepRows[s.ID], err = q.InsertStep(ctx, storedb.InsertStepParams{
				RecipeID: id, ComponentID: compID, Position: int64(si), StepKey: string(s.ID), Text: s.Text,
				DurationNs: int64(s.Duration), Passive: boolInt(s.Passive),
			})
			if err != nil {
				return wrapWrite(fmt.Sprintf("write component %d step %d (%s)", ci+1, si+1, s.ID), err)
			}
		}
	}

	for _, c := range r.Components {
		for _, s := range c.Steps {
			for di, dep := range s.DependsOn {
				err := q.InsertStepDependency(ctx, storedb.InsertStepDependencyParams{
					StepID: stepRows[s.ID], Position: int64(di), DependsOnStepID: stepRows[dep],
				})
				if err != nil {
					return wrapWrite(fmt.Sprintf("write dependency of step %s on %s", s.ID, dep), err)
				}
			}
		}
	}

	for ei, e := range r.Equipment {
		qty, err := encodeQty(e.Qty)
		if err != nil {
			return fmt.Errorf("equipment %d: %w", ei+1, err)
		}
		linkID, err := q.InsertRecipeEquipment(ctx, storedb.InsertRecipeEquipmentParams{
			RecipeID: id, Position: int64(ei), EquipmentID: int64(e.Equipment), Qty: nullString(qty),
		})
		if err != nil {
			return wrapWrite(fmt.Sprintf("write equipment %d (id %d)", ei+1, e.Equipment), err)
		}
		for si, stepID := range e.Steps {
			err := q.InsertEquipmentStep(ctx, storedb.InsertEquipmentStepParams{
				RecipeEquipmentID: linkID, Position: int64(si), StepID: stepRows[stepID],
			})
			if err != nil {
				return wrapWrite(fmt.Sprintf("write equipment %d step %s", ei+1, stepID), err)
			}
		}
	}
	return nil
}

func insertLine(ctx context.Context, q *storedb.Queries, compID int64, pos int, l recipe.IngredientLine, units *unitTable) error {
	qty, err := encodeQty(l.Amount.Qty)
	if err != nil {
		return err
	}
	unitID, err := units.id(l.Amount.Unit)
	if err != nil {
		return err
	}
	var pkgQty *string
	var pkgUnit sql.NullInt64
	if l.Amount.HasPackage {
		if pkgQty, err = encodeQty(l.Amount.Package.Qty); err != nil {
			return fmt.Errorf("package: %w", err)
		}
		if pkgUnit, err = units.id(l.Amount.Package.Unit); err != nil {
			return err
		}
	}
	var ingredient sql.NullInt64
	if l.Ingredient != 0 {
		ingredient = sql.NullInt64{Int64: int64(l.Ingredient), Valid: true}
	}
	lineID, err := q.InsertIngredientLine(ctx, storedb.InsertIngredientLineParams{
		ComponentID: compID, Position: int64(pos), Qty: nullString(qty), UnitID: unitID,
		HasPackage: boolInt(l.Amount.HasPackage), PackageQty: nullString(pkgQty), PackageUnitID: pkgUnit,
		IngredientID: ingredient, Optional: boolInt(l.Optional), Rule: string(l.Rule), Raw: l.Raw,
	})
	if err != nil {
		return wrapWrite(fmt.Sprintf("write line (ingredient id %d)", l.Ingredient), err)
	}
	for qi, ql := range l.Qualifiers {
		err := q.InsertLineQualifier(ctx, storedb.InsertLineQualifierParams{
			LineID: lineID, Position: int64(qi), Kind: string(ql.Kind), Text: ql.Text,
		})
		if err != nil {
			return wrapWrite(fmt.Sprintf("write qualifier %q", ql.Text), err)
		}
	}
	return nil
}

// clearChildren removes everything insertChildren wrote. Components cascade to
// lines, qualifiers and steps; steps cascade to dependencies.
func clearChildren(ctx context.Context, q *storedb.Queries, id int64) error {
	for _, del := range []func(context.Context, int64) error{
		q.DeleteRecipeTags,
		q.DeleteRecipeSpecialTimes,
		q.DeleteRecipeEquipment,
		q.DeleteRecipeComponents,
	} {
		if err := del(ctx, id); err != nil {
			return fmt.Errorf("clear recipe %d: %w", id, err)
		}
	}
	return nil
}
