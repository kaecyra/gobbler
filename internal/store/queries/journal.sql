-- Every query takes the household and filters on it: a recipe id from another
-- household finds nothing (ADR-00018).

-- name: InsertNote :one
INSERT INTO notes (household_id, recipe_id, body, created_at, created_by) VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: ListNotes :many
SELECT id, body, created_at, created_by FROM notes
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) ORDER BY id;

-- name: InsertMadeIt :one
INSERT INTO made_it (household_id, recipe_id, made_on, rating, comment, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ListMadeIt :many
SELECT id, made_on, rating, comment, created_at, created_by FROM made_it
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) ORDER BY made_on, id;
