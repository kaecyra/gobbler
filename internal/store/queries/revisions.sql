-- name: InsertRevision :one
INSERT INTO revisions (household_id, recipe_id, version, document, change_kind, restored_version, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertRevisionIngredient :exec
INSERT INTO revision_ingredients (household_id, revision_id, ingredient_id) VALUES (?, ?, ?);

-- name: ListRevisions :many
SELECT version, created_at, created_by, restored_version FROM revisions
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) ORDER BY version;

-- name: GetRevisionDocument :one
SELECT document FROM revisions
WHERE recipe_id = sqlc.arg(recipe_id) AND version = sqlc.arg(version) AND household_id = sqlc.arg(household_id);
