-- name: CountSnapshots :one
SELECT COUNT(*) FROM source_snapshots
WHERE recipe_id = sqlc.arg(recipe_id) AND kind = sqlc.arg(kind) AND household_id = sqlc.arg(household_id);

-- name: InsertSnapshot :exec
INSERT INTO source_snapshots (household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- A recipe may hold several snapshots of a kind once it has been re-imported
-- (ADR-00042). The one a caller wants is the newest; the first stays as the
-- original and is read by ListSnapshots.
-- name: GetSnapshot :one
SELECT kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots
WHERE recipe_id = sqlc.arg(recipe_id) AND kind = sqlc.arg(kind) AND household_id = sqlc.arg(household_id)
ORDER BY id DESC LIMIT 1;

-- The original snapshot of a kind: the first stored, which a re-import never
-- replaces (ADR-00042). A preview replays it (ADR-00048).
-- name: GetFirstSnapshot :one
SELECT kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots
WHERE recipe_id = sqlc.arg(recipe_id) AND kind = sqlc.arg(kind) AND household_id = sqlc.arg(household_id)
ORDER BY id LIMIT 1;

-- name: ListSnapshotKinds :many
SELECT kind FROM source_snapshots
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id)
GROUP BY kind ORDER BY MIN(id);

-- Every snapshot of the recipe, the original first.
-- name: ListSnapshots :many
SELECT kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) ORDER BY id;
