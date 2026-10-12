-- These re-point the shopping tables that reference ingredients(id), and the
-- check rows keyed by ingredient, during a catalog ingredient merge
-- (RegisterMergeHooks).

-- name: RepointManualItems :exec
UPDATE shopping_manual_items SET ingredient_id = sqlc.arg(winner_id) WHERE ingredient_id = sqlc.arg(loser_id);

-- name: RepointPantry :exec
UPDATE OR IGNORE shopping_pantry SET ingredient_id = sqlc.arg(winner_id) WHERE ingredient_id = sqlc.arg(loser_id);

-- name: DeletePantryIngredient :exec
DELETE FROM shopping_pantry WHERE ingredient_id = ?;

-- name: RepointChecks :exec
UPDATE OR IGNORE shopping_checks SET item_key = 'i:' || CAST(sqlc.arg(winner_id) AS TEXT)
WHERE item_key = 'i:' || CAST(sqlc.arg(loser_id) AS TEXT);

-- name: DeleteChecksForIngredient :exec
DELETE FROM shopping_checks WHERE item_key = 'i:' || CAST(sqlc.arg(loser_id) AS TEXT);
