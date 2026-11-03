-- +goose Up
-- API keys (ADR-00031): a credential for the machine-facing surface, personal
-- (owner_email, acts as that person) or household (household_id, acts for the
-- household). The apikey package owns the SQL (internal/apikey).
--
-- token_hash is the SHA-256 of the secret, so the database never holds a key
-- that could be replayed from a backup or a copy of the file; the secret is
-- shown once, when the key is made. prefix is the first characters of the
-- secret and identifies the key in a list. Exactly one of owner_email and
-- household_id is set. created_by is the member who made the key: for a
-- household key it is who has seen the secret, so leaving revokes it. Deleting
-- a household deletes its keys by the cascade. expires_at is NULL for a key
-- that never expires; last_used_at is NULL until it is used. Emails are stored
-- lower-cased, as in 00007. Timestamps are RFC 3339 UTC text.

CREATE TABLE api_keys (
    id           INTEGER PRIMARY KEY,
    token_hash   BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    prefix       TEXT NOT NULL CHECK (prefix <> ''),
    name         TEXT NOT NULL CHECK (name <> ''),
    owner_email  TEXT CHECK (owner_email IS NULL OR (owner_email <> '' AND owner_email = lower(owner_email))),
    household_id INTEGER REFERENCES households (id) ON DELETE CASCADE,
    created_by   TEXT NOT NULL CHECK (created_by <> '' AND created_by = lower(created_by)),
    created_at   TEXT NOT NULL,
    expires_at   TEXT,
    last_used_at TEXT,
    CHECK ((owner_email IS NULL) <> (household_id IS NULL))
);
-- A person's keys, and a household's, are listed by owner.
CREATE INDEX api_keys_owner_email ON api_keys (owner_email);
CREATE INDEX api_keys_household ON api_keys (household_id);
-- Removing a member revokes the household keys they made.
CREATE INDEX api_keys_created_by ON api_keys (created_by);

-- +goose Down
DROP TABLE api_keys;
