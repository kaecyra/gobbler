-- name: CreateIngredient :one
INSERT INTO ingredients (name, aisle, density_g_per_ml, usda_fdc_id, reviewed)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateIngredient :execrows
UPDATE ingredients
SET name = ?, aisle = ?, density_g_per_ml = ?, usda_fdc_id = ?, reviewed = ?
WHERE id = ?;

-- name: GetIngredient :one
SELECT id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed
FROM ingredients WHERE id = ?;

-- name: ListIngredients :many
SELECT id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed
FROM ingredients ORDER BY name;

-- name: ListUnreviewedIngredients :many
SELECT id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed
FROM ingredients WHERE reviewed = 0 ORDER BY name;

-- name: LookupIngredientID :one
SELECT id FROM ingredients WHERE name = sqlc.arg(term)
UNION ALL
SELECT ingredient_id FROM ingredient_aliases WHERE alias = sqlc.arg(term)
ORDER BY 1 LIMIT 1;

-- name: ListIngredientAliases :many
SELECT alias FROM ingredient_aliases WHERE ingredient_id = ? ORDER BY alias;

-- name: ListAllIngredientAliases :many
SELECT ingredient_id, alias FROM ingredient_aliases ORDER BY alias;

-- name: DeleteIngredientAliases :exec
DELETE FROM ingredient_aliases WHERE ingredient_id = ?;

-- name: AddIngredientAlias :exec
INSERT INTO ingredient_aliases (alias, ingredient_id) VALUES (?, ?);

-- name: DeleteIngredient :execrows
DELETE FROM ingredients WHERE id = ?;

-- name: MoveIngredientAliases :exec
UPDATE ingredient_aliases
SET ingredient_id = sqlc.arg(winner_id)
WHERE ingredient_id = sqlc.arg(loser_id);

-- LockIngredient takes the database write lock with a no-op write, so the
-- reads that follow in the same transaction see the state it will change.
-- name: LockIngredient :execrows
UPDATE ingredients SET reviewed = reviewed WHERE id = ?;

-- name: CarryIngredientData :exec
UPDATE ingredients
SET density_g_per_ml = COALESCE(density_g_per_ml, sqlc.narg(density_g_per_ml)),
    usda_fdc_id = COALESCE(usda_fdc_id, sqlc.narg(usda_fdc_id))
WHERE id = sqlc.arg(id);
