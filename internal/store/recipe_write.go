package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kaecyra/gobbler/internal/recipe"
)

// documentColumns are the scalar columns of a recipes row that come from the
// document, in the order documentSetSQL binds them.
func documentColumns(r recipe.Recipe) ([]any, error) {
	yq, err := encodeQty(r.Yield.Qty)
	if err != nil {
		return nil, fmt.Errorf("yield: %w", err)
	}
	sv, err := encodeQty(r.Servings)
	if err != nil {
		return nil, fmt.Errorf("servings: %w", err)
	}
	return []any{
		r.Name, r.Description, r.SourceURL, r.Author,
		nullString(yq), r.Yield.Unit, nullString(sv),
		nullInt(r.CaloriesPerServing), nullInt(r.Rating),
		int64(r.Times.Prep), int64(r.Times.Cook), int64(r.Times.Total),
	}, nil
}

const documentSetSQL = `name = ?, description = ?, source_url = ?, author = ?,
	yield_qty = ?, yield_unit = ?, servings = ?, calories_per_serving = ?, rating = ?,
	prep_ns = ?, cook_ns = ?, total_ns = ?`

// insertChildren writes everything below the recipes row: tags, special times,
// components with their lines and steps, step dependencies and equipment. The
// row must exist and own no children.
func insertChildren(ctx context.Context, tx *sql.Tx, id int64, r recipe.Recipe, units *unitTable) error {
	for i, tag := range r.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recipe_tags (recipe_id, position, tag) VALUES (?, ?, ?)`, id, i, tag); err != nil {
			return wrapWrite(fmt.Sprintf("write tag %q", tag), err)
		}
	}
	for i, st := range r.Times.Special {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO recipe_special_times (recipe_id, position, label, duration_ns) VALUES (?, ?, ?, ?)`,
			id, i, st.Label, int64(st.Duration)); err != nil {
			return wrapWrite(fmt.Sprintf("write special time %q", st.Label), err)
		}
	}

	stepRows := make(map[recipe.StepID]int64)
	for ci, c := range r.Components {
		res, err := tx.ExecContext(ctx, `INSERT INTO components (recipe_id, position, name) VALUES (?, ?, ?)`, id, ci, c.Name)
		if err != nil {
			return wrapWrite(fmt.Sprintf("write component %d", ci+1), err)
		}
		compID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("component %d id: %w", ci+1, err)
		}
		for li, l := range c.Lines {
			if err := insertLine(ctx, tx, compID, li, l, units); err != nil {
				return fmt.Errorf("component %d line %d: %w", ci+1, li+1, err)
			}
		}
		for si, s := range c.Steps {
			res, err := tx.ExecContext(ctx,
				`INSERT INTO steps (recipe_id, component_id, position, step_key, text, duration_ns, passive) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				id, compID, si, string(s.ID), s.Text, int64(s.Duration), boolInt(s.Passive))
			if err != nil {
				return wrapWrite(fmt.Sprintf("write component %d step %d (%s)", ci+1, si+1, s.ID), err)
			}
			if stepRows[s.ID], err = res.LastInsertId(); err != nil {
				return fmt.Errorf("step %s id: %w", s.ID, err)
			}
		}
	}

	for _, c := range r.Components {
		for _, s := range c.Steps {
			for di, dep := range s.DependsOn {
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO step_dependencies (step_id, position, depends_on_step_id) VALUES (?, ?, ?)`,
					stepRows[s.ID], di, stepRows[dep]); err != nil {
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
		res, err := tx.ExecContext(ctx,
			`INSERT INTO recipe_equipment (recipe_id, position, equipment_id, qty) VALUES (?, ?, ?, ?)`,
			id, ei, int64(e.Equipment), nullString(qty))
		if err != nil {
			return wrapWrite(fmt.Sprintf("write equipment %d (id %d)", ei+1, e.Equipment), err)
		}
		linkID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("equipment %d id: %w", ei+1, err)
		}
		for si, stepID := range e.Steps {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO recipe_equipment_steps (recipe_equipment_id, position, step_id) VALUES (?, ?, ?)`,
				linkID, si, stepRows[stepID]); err != nil {
				return wrapWrite(fmt.Sprintf("write equipment %d step %s", ei+1, stepID), err)
			}
		}
	}
	return nil
}

func insertLine(ctx context.Context, tx *sql.Tx, compID int64, pos int, l recipe.IngredientLine, units *unitTable) error {
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
	res, err := tx.ExecContext(ctx,
		`INSERT INTO ingredient_lines (component_id, position, qty, unit_id, has_package, package_qty, package_unit_id, ingredient_id, optional, rule, raw)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		compID, pos, nullString(qty), unitID, boolInt(l.Amount.HasPackage), nullString(pkgQty), pkgUnit,
		ingredient, boolInt(l.Optional), string(l.Rule), l.Raw)
	if err != nil {
		return wrapWrite(fmt.Sprintf("write line (ingredient id %d)", l.Ingredient), err)
	}
	lineID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("line id: %w", err)
	}
	for qi, q := range l.Qualifiers {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO line_qualifiers (line_id, position, kind, text) VALUES (?, ?, ?, ?)`,
			lineID, qi, string(q.Kind), q.Text); err != nil {
			return wrapWrite(fmt.Sprintf("write qualifier %q", q.Text), err)
		}
	}
	return nil
}

// clearChildren removes everything insertChildren wrote. Components cascade to
// lines, qualifiers and steps; steps cascade to dependencies.
func clearChildren(ctx context.Context, tx *sql.Tx, id int64) error {
	for _, q := range []string{
		`DELETE FROM recipe_tags WHERE recipe_id = ?`,
		`DELETE FROM recipe_special_times WHERE recipe_id = ?`,
		`DELETE FROM recipe_equipment WHERE recipe_id = ?`,
		`DELETE FROM components WHERE recipe_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("clear recipe %d: %w", id, err)
		}
	}
	return nil
}
