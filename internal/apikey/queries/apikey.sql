-- name: InsertPersonalKey :one
INSERT INTO api_keys (token_hash, prefix, name, owner_email, created_by, created_at, expires_at)
VALUES (sqlc.arg(token_hash), sqlc.arg(prefix), sqlc.arg(name), sqlc.arg(owner_email), sqlc.arg(owner_email),
        sqlc.arg(created_at), sqlc.narg(expires_at))
RETURNING id;

-- InsertHouseholdKey makes a household key only when created_by is a member of
-- the household; the membership test and the insert are one statement, so a
-- member removed meanwhile cannot make a key.
-- name: InsertHouseholdKey :one
INSERT INTO api_keys (token_hash, prefix, name, household_id, created_by, created_at, expires_at)
SELECT sqlc.arg(token_hash), sqlc.arg(prefix), sqlc.arg(name), sqlc.arg(household_id), sqlc.arg(created_by),
       sqlc.arg(created_at), sqlc.narg(expires_at)
WHERE EXISTS (SELECT 1 FROM household_members m
              WHERE m.household_id = sqlc.arg(household_id) AND m.email = sqlc.arg(created_by))
RETURNING id;

-- name: ListPersonalKeys :many
SELECT id, prefix, name, created_by, created_at, expires_at, last_used_at
FROM api_keys WHERE owner_email = ? ORDER BY created_at, id;

-- name: ListHouseholdKeys :many
SELECT id, prefix, name, created_by, created_at, expires_at, last_used_at
FROM api_keys WHERE household_id = ? ORDER BY created_at, id;

-- name: RevokePersonalKey :execrows
DELETE FROM api_keys WHERE id = sqlc.arg(id) AND owner_email = sqlc.arg(owner_email);

-- name: RevokeHouseholdKey :execrows
DELETE FROM api_keys WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id);

-- RevokeMemberKeys deletes a former member's personal keys and the household
-- keys they made, because they have seen those secrets (ADR-00031).
-- name: RevokeMemberKeys :execrows
DELETE FROM api_keys
WHERE owner_email = sqlc.arg(email) OR (household_id IS NOT NULL AND created_by = sqlc.arg(email));
