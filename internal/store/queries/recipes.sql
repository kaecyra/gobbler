-- Every query on a recipe takes the household and filters or stamps it: a
-- recipe id from another household finds nothing (ADR-00018). ListUnits is the
-- one global read, the catalog's unit table.

-- name: InsertRecipe :one
INSERT INTO recipes (household_id, name, description, source_url, author, yield_qty, yield_unit, servings, calories_per_serving, rating,
  prep_ns, cook_ns, total_ns, cloned_from_recipe_id, cloned_from_version, created_at, created_by, updated_at, updated_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateRecipeDocument :exec
UPDATE recipes
SET name = ?, description = ?, source_url = ?, author = ?,
    yield_qty = ?, yield_unit = ?, servings = ?, calories_per_serving = ?, rating = ?,
    prep_ns = ?, cook_ns = ?, total_ns = ?,
    version = version + 1, updated_at = ?, updated_by = ?
WHERE id = ? AND household_id = ?;

-- LockRecipe takes the database write lock with a no-op write, so the reads
-- that follow in the same transaction see the state it will change.
-- name: LockRecipe :execrows
UPDATE recipes SET version = version WHERE id = ? AND household_id = ?;

-- name: DeleteRecipe :execrows
DELETE FROM recipes WHERE id = ? AND household_id = ?;

-- name: GetRecipeInfo :one
SELECT version, created_at, created_by, updated_at, updated_by, cloned_from_recipe_id, cloned_from_version
FROM recipes WHERE id = ? AND household_id = ?;

-- name: GetRecipeDocument :one
SELECT name, description, source_url, author, yield_qty, yield_unit, servings, calories_per_serving, rating, prep_ns, cook_ns, total_ns
FROM recipes WHERE id = ? AND household_id = ?;

-- name: ListUnits :many
SELECT id, name, system, dimension, factor FROM units;

-- name: InsertRecipeTag :exec
INSERT INTO recipe_tags (household_id, recipe_id, position, tag) VALUES (?, ?, ?, ?);

-- name: InsertSpecialTime :exec
INSERT INTO recipe_special_times (household_id, recipe_id, position, label, duration_ns) VALUES (?, ?, ?, ?, ?);

-- name: InsertComponent :one
INSERT INTO components (household_id, recipe_id, position, name) VALUES (?, ?, ?, ?)
RETURNING id;

-- name: InsertIngredientLine :one
INSERT INTO ingredient_lines (household_id, component_id, position, qty, unit_id, has_package, package_qty, package_unit_id, ingredient_id, optional, rule, raw)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertLineQualifier :exec
INSERT INTO line_qualifiers (household_id, line_id, position, kind, text) VALUES (?, ?, ?, ?, ?);

-- name: InsertStep :one
INSERT INTO steps (household_id, recipe_id, component_id, position, step_key, text, duration_ns, passive)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertStepDependency :exec
INSERT INTO step_dependencies (household_id, step_id, position, depends_on_step_id) VALUES (?, ?, ?, ?);

-- name: InsertRecipeEquipment :one
INSERT INTO recipe_equipment (household_id, recipe_id, position, equipment_id, qty) VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertEquipmentStep :exec
INSERT INTO recipe_equipment_steps (household_id, recipe_equipment_id, position, step_id) VALUES (?, ?, ?, ?);

-- Components cascade to lines, qualifiers and steps; steps cascade to
-- dependencies. The four deletes below clear everything InsertRecipeTag
-- through InsertEquipmentStep wrote.

-- name: DeleteRecipeTags :exec
DELETE FROM recipe_tags WHERE recipe_id = ? AND household_id = ?;

-- name: DeleteRecipeSpecialTimes :exec
DELETE FROM recipe_special_times WHERE recipe_id = ? AND household_id = ?;

-- name: DeleteRecipeEquipment :exec
DELETE FROM recipe_equipment WHERE recipe_id = ? AND household_id = ?;

-- name: DeleteRecipeComponents :exec
DELETE FROM components WHERE recipe_id = ? AND household_id = ?;

-- name: ListRecipeTags :many
SELECT tag FROM recipe_tags WHERE recipe_id = ? AND household_id = ? ORDER BY position;

-- name: ListSpecialTimes :many
SELECT label, duration_ns FROM recipe_special_times WHERE recipe_id = ? AND household_id = ? ORDER BY position;

-- name: ListComponents :many
SELECT id, name FROM components WHERE recipe_id = ? AND household_id = ? ORDER BY position;

-- name: ListIngredientLines :many
SELECT l.id, l.component_id, l.qty, l.unit_id, l.has_package, l.package_qty, l.package_unit_id, l.ingredient_id, l.optional, l.rule, l.raw
FROM ingredient_lines l JOIN components c ON c.id = l.component_id
WHERE c.recipe_id = ? AND c.household_id = ? ORDER BY c.position, l.position;

-- name: ListLineQualifiers :many
SELECT lq.line_id, lq.kind, lq.text FROM line_qualifiers lq
JOIN ingredient_lines l ON l.id = lq.line_id JOIN components c ON c.id = l.component_id
WHERE c.recipe_id = ? AND c.household_id = ? ORDER BY lq.line_id, lq.position;

-- name: ListSteps :many
SELECT s.id, s.component_id, s.step_key, s.text, s.duration_ns, s.passive
FROM steps s JOIN components c ON c.id = s.component_id
WHERE s.recipe_id = ? AND s.household_id = ? ORDER BY c.position, s.position;

-- name: ListStepDependencies :many
SELECT d.step_id, d.depends_on_step_id FROM step_dependencies d
JOIN steps s ON s.id = d.step_id WHERE s.recipe_id = ? AND s.household_id = ? ORDER BY d.step_id, d.position;

-- name: ListRecipeEquipment :many
SELECT id, equipment_id, qty FROM recipe_equipment WHERE recipe_id = ? AND household_id = ? ORDER BY position;

-- name: ListEquipmentSteps :many
SELECT es.recipe_equipment_id, s.step_key FROM recipe_equipment_steps es
JOIN recipe_equipment e ON e.id = es.recipe_equipment_id JOIN steps s ON s.id = es.step_id
WHERE e.recipe_id = ? AND e.household_id = ? ORDER BY es.recipe_equipment_id, es.position;
