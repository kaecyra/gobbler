-- These count the shopping rows a catalog ingredient merge re-points
-- (RegisterUsageCounters): the same tables and check keys as merge.sql.

-- name: CountIngredientManualItems :one
SELECT COUNT(*) FROM shopping_manual_items WHERE ingredient_id = ?;

-- name: CountIngredientPantry :one
SELECT COUNT(*) FROM shopping_pantry WHERE ingredient_id = ?;

-- name: CountIngredientChecks :one
SELECT COUNT(*) FROM shopping_checks WHERE item_key = 'i:' || CAST(sqlc.arg(ingredient_id) AS TEXT);
