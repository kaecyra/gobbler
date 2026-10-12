-- name: InsertList :one
INSERT INTO shopping_lists (name, created_at) VALUES (?, ?) RETURNING id;

-- name: GetList :one
SELECT id, name, created_at, include_optional FROM shopping_lists WHERE id = ?;

-- name: ListLists :many
SELECT id, name, created_at, include_optional FROM shopping_lists ORDER BY id;

-- name: ListListIDs :many
SELECT id FROM shopping_lists ORDER BY id;

-- name: SetIncludeOptional :execrows
UPDATE shopping_lists SET include_optional = sqlc.arg(include_optional) WHERE id = sqlc.arg(id) AND include_optional <> sqlc.arg(include_optional);

-- name: RenameList :execrows
UPDATE shopping_lists SET name = ? WHERE id = ?;

-- LockList takes the database write lock with a no-op write, so the reads
-- that follow in the same transaction see the state it will change.
-- name: LockList :execrows
UPDATE shopping_lists SET name = name WHERE id = ?;

-- name: DeleteList :execrows
DELETE FROM shopping_lists WHERE id = ?;

-- name: InsertEntry :one
INSERT INTO shopping_entries (list_id, recipe_id, multiplier) VALUES (?, ?, ?) RETURNING id;

-- name: ListEntries :many
SELECT id, recipe_id, multiplier FROM shopping_entries WHERE list_id = ? ORDER BY id;

-- name: SetEntryMultiplier :one
UPDATE shopping_entries SET multiplier = ? WHERE id = ? RETURNING list_id;

-- name: DeleteEntry :one
DELETE FROM shopping_entries WHERE id = ? RETURNING list_id;

-- name: InsertManualItem :one
INSERT INTO shopping_manual_items (list_id, text, ingredient_id, qty, unit_id, has_package, package_qty, package_unit_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: ListManualItems :many
SELECT id, text, ingredient_id, qty, unit_id, has_package, package_qty, package_unit_id
FROM shopping_manual_items WHERE list_id = ? ORDER BY id;

-- name: UpdateManualItem :one
UPDATE shopping_manual_items
SET text = ?, ingredient_id = ?, qty = ?, unit_id = ?, has_package = ?, package_qty = ?, package_unit_id = ?
WHERE id = ? RETURNING list_id;

-- name: DeleteManualItem :one
DELETE FROM shopping_manual_items WHERE id = ? RETURNING list_id;

-- name: AddPantryIngredient :execrows
INSERT INTO shopping_pantry (ingredient_id) VALUES (?) ON CONFLICT (ingredient_id) DO NOTHING;

-- name: RemovePantryIngredient :execrows
DELETE FROM shopping_pantry WHERE ingredient_id = ?;

-- name: ListPantry :many
SELECT ingredient_id FROM shopping_pantry ORDER BY ingredient_id;

-- name: SetChecked :execrows
INSERT INTO shopping_checks (list_id, item_key, checked_at) VALUES (?, ?, ?)
ON CONFLICT (list_id, item_key) DO NOTHING;

-- name: ClearChecked :execrows
DELETE FROM shopping_checks WHERE list_id = ? AND item_key = ?;

-- name: ListChecked :many
SELECT item_key FROM shopping_checks WHERE list_id = ? ORDER BY item_key;
