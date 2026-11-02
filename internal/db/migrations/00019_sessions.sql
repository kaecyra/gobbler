-- +goose Up
-- Sign-in sessions (ADR-00030): a successful sign-in creates a row, the cookie
-- carries only the random token that names it, and signing out deletes it. The
-- session package owns the SQL (internal/session).
--
-- token_hash is the SHA-256 of the token, so the database never holds a token
-- that could be replayed from a backup or a copy of the file. A session lasts
-- a fixed time from sign-in: expires_at is set once and never moved, and
-- last_used_at only records activity. Emails are stored lower-cased, as in
-- 00007. Timestamps are RFC 3339 UTC text.

CREATE TABLE sessions (
    token_hash   BLOB NOT NULL PRIMARY KEY CHECK (length(token_hash) = 32),
    email        TEXT NOT NULL CHECK (email <> '' AND email = lower(email)),
    created_at   TEXT NOT NULL,
    last_used_at TEXT NOT NULL,
    expires_at   TEXT NOT NULL
);
-- Removing a member ends every session of their email at once.
CREATE INDEX sessions_email ON sessions (email);

-- +goose Down
DROP TABLE sessions;
