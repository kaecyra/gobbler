-- FindKeyByHash reads a key by the hash of its secret. For a household key
-- household_name is the household's name; for a personal key it is NULL.
-- name: FindKeyByHash :one
SELECT k.id, k.prefix, k.created_at, k.owner_email, k.household_id, k.expires_at, h.name AS household_name
FROM api_keys k LEFT JOIN households h ON h.id = k.household_id
WHERE k.token_hash = sqlc.arg(token_hash);

-- TouchKey stamps the last use, but at most once a minute so a busy agent does
-- not write on every request.
-- name: TouchKey :exec
UPDATE api_keys SET last_used_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND (last_used_at IS NULL OR last_used_at < sqlc.arg(older_than));

-- InsertKeyWrite records a write with a snapshot of the key that made it, read
-- in the same statement: a revoked key's id can be given to a later key. No row
-- is inserted when the key is gone.
-- name: InsertKeyWrite :execrows
INSERT INTO api_key_writes (key_id, key_prefix, key_owner, key_created_at, household_id, tool, target, created_at)
SELECT k.id, k.prefix, coalesce(k.owner_email, 'household:' || k.household_id), k.created_at,
       sqlc.arg(household_id), sqlc.arg(tool), sqlc.arg(target), sqlc.arg(created_at)
FROM api_keys k WHERE k.id = sqlc.arg(key_id);

-- ListKeyWrites matches the snapshot as well as the id, so a key given a
-- revoked key's id does not list that key's writes.
-- name: ListKeyWrites :many
SELECT id, key_id, household_id, tool, target, created_at
FROM api_key_writes
WHERE key_id = sqlc.arg(key_id) AND key_prefix = sqlc.arg(key_prefix)
  AND key_owner = sqlc.arg(key_owner) AND key_created_at = sqlc.arg(key_created_at)
ORDER BY id;
