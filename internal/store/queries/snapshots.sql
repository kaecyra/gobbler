-- name: CountSnapshots :one
SELECT COUNT(*) FROM source_snapshots
WHERE recipe_id = sqlc.arg(recipe_id) AND kind = sqlc.arg(kind) AND household_id = sqlc.arg(household_id);

-- name: InsertSnapshot :exec
INSERT INTO source_snapshots (household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSnapshot :one
SELECT kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots
WHERE recipe_id = sqlc.arg(recipe_id) AND kind = sqlc.arg(kind) AND household_id = sqlc.arg(household_id);

-- name: ListSnapshotKinds :many
SELECT kind FROM source_snapshots
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) ORDER BY id;
