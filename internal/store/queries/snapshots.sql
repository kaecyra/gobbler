-- name: CountSnapshots :one
SELECT COUNT(*) FROM source_snapshots WHERE recipe_id = ? AND kind = ?;

-- name: InsertSnapshot :exec
INSERT INTO source_snapshots (recipe_id, kind, content, sha256, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetSnapshot :one
SELECT kind, content, sha256, created_at, created_by FROM source_snapshots WHERE recipe_id = ? AND kind = ?;

-- name: ListSnapshotKinds :many
SELECT kind FROM source_snapshots WHERE recipe_id = ? ORDER BY id;
