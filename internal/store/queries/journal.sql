-- name: InsertNote :one
INSERT INTO notes (recipe_id, body, created_at, created_by) VALUES (?, ?, ?, ?)
RETURNING id;

-- name: ListNotes :many
SELECT id, body, created_at, created_by FROM notes WHERE recipe_id = ? ORDER BY id;

-- name: InsertMadeIt :one
INSERT INTO made_it (recipe_id, made_on, rating, comment, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: ListMadeIt :many
SELECT id, made_on, rating, comment, created_at, created_by FROM made_it
WHERE recipe_id = ? ORDER BY made_on, id;
