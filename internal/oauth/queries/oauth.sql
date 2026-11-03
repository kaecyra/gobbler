-- name: InsertClient :exec
INSERT INTO oauth_clients (client_id, client_name, redirect_uris, created_at)
VALUES (sqlc.arg(client_id), sqlc.arg(client_name), sqlc.arg(redirect_uris), sqlc.arg(created_at));

-- name: GetClient :one
SELECT client_id, client_name, redirect_uris, created_at FROM oauth_clients WHERE client_id = ?;

-- PruneClients deletes clients that registered before the cutoff and never
-- received a grant, so the anonymous registration endpoint cannot fill the
-- table for good.
-- name: PruneClients :execrows
DELETE FROM oauth_clients
WHERE oauth_clients.created_at < sqlc.arg(cutoff)
  AND NOT EXISTS (SELECT 1 FROM oauth_grants g WHERE g.client_id = oauth_clients.client_id);

-- name: CountClients :one
SELECT count(*) FROM oauth_clients;

-- name: InsertCode :exec
INSERT INTO oauth_codes (code_hash, owner_email, client_id, client_name, redirect_uri, code_challenge, resource, created_at, expires_at)
VALUES (sqlc.arg(code_hash), sqlc.arg(owner_email), sqlc.arg(client_id), sqlc.arg(client_name), sqlc.arg(redirect_uri),
        sqlc.arg(code_challenge), sqlc.arg(resource), sqlc.arg(created_at), sqlc.arg(expires_at));

-- name: GetCode :one
SELECT owner_email, client_id, client_name, redirect_uri, code_challenge, resource, expires_at, grant_id
FROM oauth_codes WHERE code_hash = ?;

-- name: DeleteCode :exec
DELETE FROM oauth_codes WHERE code_hash = ?;

-- name: MarkCodeUsed :exec
UPDATE oauth_codes SET grant_id = sqlc.arg(grant_id) WHERE code_hash = sqlc.arg(code_hash);

-- name: PruneCodes :exec
DELETE FROM oauth_codes WHERE expires_at < ?;

-- name: InsertGrant :one
INSERT INTO oauth_grants (owner_email, client_id, client_name, return_host, resource, created_at)
VALUES (sqlc.arg(owner_email), sqlc.arg(client_id), sqlc.arg(client_name), sqlc.arg(return_host), sqlc.arg(resource), sqlc.arg(created_at))
RETURNING id;

-- name: DeleteGrant :execrows
DELETE FROM oauth_grants WHERE id = ?;

-- name: ListGrants :many
SELECT id, client_id, client_name, return_host, created_at
FROM oauth_grants WHERE owner_email = ? ORDER BY created_at, id;

-- name: RevokeGrant :execrows
DELETE FROM oauth_grants WHERE id = sqlc.arg(id) AND owner_email = sqlc.arg(owner_email);

-- RevokeMemberGrants deletes a former member's grants (ADR-00034).
-- name: RevokeMemberGrants :execrows
DELETE FROM oauth_grants WHERE owner_email = ?;

-- name: RevokeMemberCodes :execrows
DELETE FROM oauth_codes WHERE owner_email = ?;

-- name: InsertToken :exec
INSERT INTO oauth_tokens (grant_id, kind, token_hash, expires_at)
VALUES (sqlc.arg(grant_id), sqlc.arg(kind), sqlc.arg(token_hash), sqlc.arg(expires_at));

-- name: GetRefreshToken :one
SELECT t.id, t.grant_id, t.expires_at, t.used_at, g.client_id, g.resource
FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
WHERE t.kind = 'refresh' AND t.token_hash = ?;

-- name: MarkTokenUsed :exec
UPDATE oauth_tokens SET used_at = sqlc.arg(used_at) WHERE id = sqlc.arg(id);

-- PruneTokens deletes a grant's tokens that have expired.
-- name: PruneTokens :exec
DELETE FROM oauth_tokens WHERE grant_id = sqlc.arg(grant_id) AND expires_at < sqlc.arg(now);

-- name: GetAccessToken :one
SELECT g.id AS grant_id, g.owner_email, g.client_id, g.created_at AS grant_created_at, g.resource, t.expires_at
FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
WHERE t.kind = 'access' AND t.token_hash = ?;

-- InsertGrantWrite records a write with a snapshot of the grant that made it,
-- read in the same statement. No row is inserted when the grant is gone.
-- name: InsertGrantWrite :execrows
INSERT INTO oauth_grant_writes (grant_id, grant_client_id, grant_owner, grant_created_at, household_id, tool, target, created_at)
SELECT g.id, g.client_id, g.owner_email, g.created_at, sqlc.arg(household_id), sqlc.arg(tool), sqlc.arg(target), sqlc.arg(created_at)
FROM oauth_grants g WHERE g.id = sqlc.arg(grant_id);

-- ListGrantWrites matches the snapshot as well as the id: a revoked grant's id
-- can be given to a later grant, which must not see the earlier one's writes.
-- name: ListGrantWrites :many
SELECT id, grant_id, household_id, tool, target, created_at
FROM oauth_grant_writes
WHERE grant_id = sqlc.arg(grant_id) AND grant_client_id = sqlc.arg(client_id)
  AND grant_owner = sqlc.arg(owner) AND grant_created_at = sqlc.arg(grant_created_at)
ORDER BY id;
