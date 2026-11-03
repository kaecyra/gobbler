-- name: InsertSession :exec
INSERT INTO sessions (token_hash, email, created_at, last_used_at, expires_at, user_agent, client_ip)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- GetLiveSession reads a session that has not expired. A session past its
-- expiry matches nothing.
-- name: GetLiveSession :one
SELECT email, last_used_at FROM sessions
WHERE token_hash = sqlc.arg(token_hash) AND expires_at > sqlc.arg(now);

-- TouchSession records that a session was used. The expiry itself is never
-- moved: a session lasts a fixed time from sign-in (ADR-00030).
-- name: TouchSession :exec
UPDATE sessions SET last_used_at = sqlc.arg(now) WHERE token_hash = sqlc.arg(token_hash);

-- name: DeleteSession :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteExpiredSession :exec
DELETE FROM sessions WHERE token_hash = sqlc.arg(token_hash) AND expires_at <= sqlc.arg(now);

-- name: DeleteSessionsForEmail :execrows
DELETE FROM sessions WHERE email = ?;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= sqlc.arg(now);

-- ListLiveSessionsForEmail is the person's sessions that have not expired,
-- newest first. The token hash is selected so the caller can mark the current
-- one and name a session to end; it is not a token and opens nothing.
-- name: ListLiveSessionsForEmail :many
SELECT token_hash, created_at, last_used_at, expires_at, user_agent, client_ip FROM sessions
WHERE email = sqlc.arg(email) AND expires_at > sqlc.arg(now)
ORDER BY created_at DESC, token_hash;

-- DeleteSessionOfEmail ends one session, and only if it is the email's.
-- name: DeleteSessionOfEmail :execrows
DELETE FROM sessions WHERE token_hash = sqlc.arg(token_hash) AND email = sqlc.arg(email);

-- DeleteOtherSessionsOfEmail ends every session of the email but one.
-- name: DeleteOtherSessionsOfEmail :execrows
DELETE FROM sessions WHERE email = sqlc.arg(email) AND token_hash <> sqlc.arg(keep_hash);
