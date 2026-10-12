package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/quantity"
	"github.com/kaecyra/gobbler/internal/recipe"
)

// loadDocument reads the recipe document with the given id. Empty collections
// come back nil. A missing recipe is ErrNotFound.
func loadDocument(ctx context.Context, q queryer, id int64, units *unitTable) (recipe.Recipe, error) {
	var (
		r                  recipe.Recipe
		yieldQty, servings sql.NullString
		calories, rating   sql.NullInt64
		prep, cook, total  int64
	)
	err := q.QueryRowContext(ctx,
		`SELECT name, description, source_url, author, yield_qty, yield_unit, servings, calories_per_serving, rating, prep_ns, cook_ns, total_ns
		 FROM recipes WHERE id = ?`, id).
		Scan(&r.Name, &r.Description, &r.SourceURL, &r.Author, &yieldQty, &r.Yield.Unit, &servings, &calories, &rating, &prep, &cook, &total)
	if errors.Is(err, sql.ErrNoRows) {
		return recipe.Recipe{}, fmt.Errorf("%w: recipe %d", ErrNotFound, id)
	}
	if err != nil {
		return recipe.Recipe{}, fmt.Errorf("read recipe %d: %w", id, err)
	}
	if r.Yield.Qty, err = decodeQty(stringPtr(yieldQty)); err != nil {
		return recipe.Recipe{}, fmt.Errorf("recipe %d yield: %w", id, err)
	}
	if r.Servings, err = decodeQty(stringPtr(servings)); err != nil {
		return recipe.Recipe{}, fmt.Errorf("recipe %d servings: %w", id, err)
	}
	r.CaloriesPerServing, r.Rating = intPtr(calories), intPtr(rating)
	r.Times = recipe.Times{Prep: time.Duration(prep), Cook: time.Duration(cook), Total: time.Duration(total)}

	if r.Tags, err = loadTags(ctx, q, id); err != nil {
		return recipe.Recipe{}, err
	}
	if r.Times.Special, err = loadSpecialTimes(ctx, q, id); err != nil {
		return recipe.Recipe{}, err
	}
	if r.Components, err = loadComponents(ctx, q, id, units); err != nil {
		return recipe.Recipe{}, err
	}
	if r.Equipment, err = loadEquipment(ctx, q, id); err != nil {
		return recipe.Recipe{}, err
	}
	return r, nil
}

// collect runs a query and scans every row with scan, wrapping failures with
// what the caller was reading.
func collect(ctx context.Context, q queryer, what, query string, args []any, scan func(rows *sql.Rows) error) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("read %s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return fmt.Errorf("read %s: %w", what, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read %s: %w", what, err)
	}
	return nil
}

func loadTags(ctx context.Context, q queryer, id int64) ([]string, error) {
	var tags []string
	err := collect(ctx, q, "tags", `SELECT tag FROM recipe_tags WHERE recipe_id = ? ORDER BY position`, []any{id}, func(rows *sql.Rows) error {
		var t string
		if err := rows.Scan(&t); err != nil {
			return err
		}
		tags = append(tags, t)
		return nil
	})
	return tags, err
}

func loadSpecialTimes(ctx context.Context, q queryer, id int64) ([]recipe.SpecialTime, error) {
	var out []recipe.SpecialTime
	err := collect(ctx, q, "special times", `SELECT label, duration_ns FROM recipe_special_times WHERE recipe_id = ? ORDER BY position`, []any{id}, func(rows *sql.Rows) error {
		var (
			st recipe.SpecialTime
			ns int64
		)
		if err := rows.Scan(&st.Label, &ns); err != nil {
			return err
		}
		st.Duration = time.Duration(ns)
		out = append(out, st)
		return nil
	})
	return out, err
}

func loadComponents(ctx context.Context, q queryer, id int64, units *unitTable) ([]recipe.Component, error) {
	var comps []recipe.Component
	compIdx := map[int64]int{}
	err := collect(ctx, q, "components", `SELECT id, name FROM components WHERE recipe_id = ? ORDER BY position`, []any{id}, func(rows *sql.Rows) error {
		var (
			cid int64
			c   recipe.Component
		)
		if err := rows.Scan(&cid, &c.Name); err != nil {
			return err
		}
		compIdx[cid] = len(comps)
		comps = append(comps, c)
		return nil
	})
	if err != nil {
		return nil, err
	}

	lineIdx := map[int64][2]int{} // line id -> component index, line index
	err = collect(ctx, q, "ingredient lines",
		`SELECT l.id, l.component_id, l.qty, l.unit_id, l.has_package, l.package_qty, l.package_unit_id, l.ingredient_id, l.optional, l.rule, l.raw
		 FROM ingredient_lines l JOIN components c ON c.id = l.component_id
		 WHERE c.recipe_id = ? ORDER BY c.position, l.position`, []any{id}, func(rows *sql.Rows) error {
			var (
				lid, cid                    int64
				qty, pkgQty                 sql.NullString
				unitID, pkgUnitID, ingredID sql.NullInt64
				hasPkg, optional            int
				l                           recipe.IngredientLine
				rule                        string
			)
			if err := rows.Scan(&lid, &cid, &qty, &unitID, &hasPkg, &pkgQty, &pkgUnitID, &ingredID, &optional, &rule, &l.Raw); err != nil {
				return err
			}
			var err error
			if l.Amount.Qty, err = decodeQty(stringPtr(qty)); err != nil {
				return err
			}
			if l.Amount.Unit, err = units.unit(unitID); err != nil {
				return err
			}
			if hasPkg == 1 {
				l.Amount.HasPackage = true
				if l.Amount.Package.Qty, err = decodeQty(stringPtr(pkgQty)); err != nil {
					return err
				}
				if l.Amount.Package.Unit, err = units.unit(pkgUnitID); err != nil {
					return err
				}
			}
			l.Ingredient = recipe.IngredientID(ingredID.Int64)
			l.Optional = optional == 1
			l.Rule = quantity.Rule(rule)
			ci, err := lookup(compIdx, cid, "component")
			if err != nil {
				return err
			}
			lineIdx[lid] = [2]int{ci, len(comps[ci].Lines)}
			comps[ci].Lines = append(comps[ci].Lines, l)
			return nil
		})
	if err != nil {
		return nil, err
	}

	err = collect(ctx, q, "line qualifiers",
		`SELECT lq.line_id, lq.kind, lq.text FROM line_qualifiers lq
		 JOIN ingredient_lines l ON l.id = lq.line_id JOIN components c ON c.id = l.component_id
		 WHERE c.recipe_id = ? ORDER BY lq.line_id, lq.position`, []any{id}, func(rows *sql.Rows) error {
			var (
				lid  int64
				kind string
				text string
			)
			if err := rows.Scan(&lid, &kind, &text); err != nil {
				return err
			}
			at, err := lookup(lineIdx, lid, "ingredient line")
			if err != nil {
				return err
			}
			line := &comps[at[0]].Lines[at[1]]
			line.Qualifiers = append(line.Qualifiers, recipe.Qualifier{Kind: recipe.QualifierKind(kind), Text: text})
			return nil
		})
	if err != nil {
		return nil, err
	}

	stepKeys := map[int64]recipe.StepID{}
	stepIdx := map[int64][2]int{}
	err = collect(ctx, q, "steps",
		`SELECT s.id, s.component_id, s.step_key, s.text, s.duration_ns, s.passive
		 FROM steps s JOIN components c ON c.id = s.component_id
		 WHERE s.recipe_id = ? ORDER BY c.position, s.position`, []any{id}, func(rows *sql.Rows) error {
			var (
				sid, cid, ns int64
				key          string
				passive      int
				s            recipe.Step
			)
			if err := rows.Scan(&sid, &cid, &key, &s.Text, &ns, &passive); err != nil {
				return err
			}
			s.ID, s.Duration, s.Passive = recipe.StepID(key), time.Duration(ns), passive == 1
			ci, err := lookup(compIdx, cid, "component")
			if err != nil {
				return err
			}
			stepKeys[sid] = s.ID
			stepIdx[sid] = [2]int{ci, len(comps[ci].Steps)}
			comps[ci].Steps = append(comps[ci].Steps, s)
			return nil
		})
	if err != nil {
		return nil, err
	}

	err = collect(ctx, q, "step dependencies",
		`SELECT d.step_id, d.depends_on_step_id FROM step_dependencies d
		 JOIN steps s ON s.id = d.step_id WHERE s.recipe_id = ? ORDER BY d.step_id, d.position`, []any{id}, func(rows *sql.Rows) error {
			var sid, dep int64
			if err := rows.Scan(&sid, &dep); err != nil {
				return err
			}
			at, err := lookup(stepIdx, sid, "step")
			if err != nil {
				return err
			}
			depKey, err := lookup(stepKeys, dep, "step")
			if err != nil {
				return fmt.Errorf("dependency of step %s: %w", stepKeys[sid], err)
			}
			step := &comps[at[0]].Steps[at[1]]
			step.DependsOn = append(step.DependsOn, depKey)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return comps, nil
}

func loadEquipment(ctx context.Context, q queryer, id int64) ([]recipe.EquipmentLink, error) {
	var out []recipe.EquipmentLink
	rowIdx := map[int64]int{}
	err := collect(ctx, q, "equipment", `SELECT id, equipment_id, qty FROM recipe_equipment WHERE recipe_id = ? ORDER BY position`, []any{id}, func(rows *sql.Rows) error {
		var (
			rid, eid int64
			qty      sql.NullString
		)
		if err := rows.Scan(&rid, &eid, &qty); err != nil {
			return err
		}
		q, err := decodeQty(stringPtr(qty))
		if err != nil {
			return err
		}
		rowIdx[rid] = len(out)
		out = append(out, recipe.EquipmentLink{Equipment: recipe.EquipmentID(eid), Qty: q})
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = collect(ctx, q, "equipment steps",
		`SELECT es.recipe_equipment_id, s.step_key FROM recipe_equipment_steps es
		 JOIN recipe_equipment e ON e.id = es.recipe_equipment_id JOIN steps s ON s.id = es.step_id
		 WHERE e.recipe_id = ? ORDER BY es.recipe_equipment_id, es.position`, []any{id}, func(rows *sql.Rows) error {
			var (
				rid int64
				key string
			)
			if err := rows.Scan(&rid, &key); err != nil {
				return err
			}
			at, err := lookup(rowIdx, rid, "equipment link")
			if err != nil {
				return err
			}
			l := &out[at]
			l.Steps = append(l.Steps, recipe.StepID(key))
			return nil
		})
	return out, err
}
