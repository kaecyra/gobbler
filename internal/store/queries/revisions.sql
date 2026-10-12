-- name: InsertRevision :one
INSERT INTO revisions (recipe_id, version, document, change_kind, restored_version, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertRevisionIngredient :exec
INSERT INTO revision_ingredients (revision_id, ingredient_id) VALUES (?, ?);

-- name: ListRevisions :many
SELECT version, created_at, created_by, restored_version FROM revisions WHERE recipe_id = ? ORDER BY version;

-- name: GetRevisionDocument :one
SELECT document FROM revisions WHERE recipe_id = ? AND version = ?;
