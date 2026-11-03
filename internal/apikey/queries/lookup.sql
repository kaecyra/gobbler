-- FindKeyByHash reads a key by the hash of its secret. For a household key
-- household_name is the household's name; for a personal key it is NULL.
-- name: FindKeyByHash :one
SELECT k.id, k.owner_email, k.household_id, k.expires_at, h.name AS household_name
FROM api_keys k LEFT JOIN households h ON h.id = k.household_id
WHERE k.token_hash = sqlc.arg(token_hash);

-- TouchKey stamps the last use, but at most once a minute so a busy agent does
-- not write on every request.
-- name: TouchKey :exec
UPDATE api_keys SET last_used_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND (last_used_at IS NULL OR last_used_at < sqlc.arg(older_than));

-- name: InsertKeyWrite :exec
INSERT INTO api_key_writes (key_id, household_id, tool, target, created_at)
VALUES (sqlc.arg(key_id), sqlc.arg(household_id), sqlc.arg(tool), sqlc.arg(target), sqlc.arg(created_at));

-- name: ListKeyWrites :many
SELECT id, key_id, household_id, tool, target, created_at
FROM api_key_writes WHERE key_id = ? ORDER BY id;
