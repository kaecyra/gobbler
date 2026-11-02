-- name: InsertSession :exec
INSERT INTO sessions (token_hash, email, created_at, last_used_at, expires_at)
VALUES (?, ?, ?, ?, ?);

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
