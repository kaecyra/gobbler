-- +goose Up
-- OAuth 2.1 authorization server for MCP clients (ADR-00034). The oauth
-- package owns the SQL (internal/oauth).
--
-- oauth_clients holds clients that registered themselves (Dynamic Client
-- Registration). A client that names a Client ID Metadata Document is fetched
-- when needed and never stored. redirect_uris is a JSON array of strings.
--
-- A grant is one person's approval of one client, acting as that person with
-- the reach of a personal key. owner_email is lower-cased, as in 00007. It has
-- no foreign key because a person is an email, not a row. Revoking a grant
-- deletes it, and the tokens and codes that belong to it go by the cascade.
-- client_id has no foreign key either, since a metadata client is not stored.
-- resource is the audience every token of the grant is bound to.
--
-- Codes, access tokens and refresh tokens are stored as SHA-256 hashes only,
-- so a copy of the file replays nothing. A code is used once: exchanging it
-- sets grant_id, and presenting it again revokes that grant. A refresh token
-- is rotated by setting used_at; presenting a used one revokes the grant.
-- Timestamps are fixed-width RFC 3339 UTC text, so text comparison orders them.

CREATE TABLE oauth_clients (
    client_id     TEXT PRIMARY KEY CHECK (client_id <> ''),
    client_name   TEXT NOT NULL,
    redirect_uris TEXT NOT NULL,
    created_at    TEXT NOT NULL
);

CREATE TABLE oauth_grants (
    id           INTEGER PRIMARY KEY,
    owner_email  TEXT NOT NULL CHECK (owner_email <> '' AND owner_email = lower(owner_email)),
    client_id    TEXT NOT NULL CHECK (client_id <> ''),
    client_name  TEXT NOT NULL,
    return_host  TEXT NOT NULL,
    resource     TEXT NOT NULL CHECK (resource <> ''),
    created_at   TEXT NOT NULL
);
CREATE INDEX oauth_grants_owner ON oauth_grants (owner_email);
CREATE INDEX oauth_grants_client ON oauth_grants (client_id);

CREATE TABLE oauth_codes (
    code_hash      BLOB PRIMARY KEY CHECK (length(code_hash) = 32),
    owner_email    TEXT NOT NULL CHECK (owner_email <> '' AND owner_email = lower(owner_email)),
    client_id      TEXT NOT NULL,
    client_name    TEXT NOT NULL,
    redirect_uri   TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    resource       TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    expires_at     TEXT NOT NULL,
    grant_id       INTEGER REFERENCES oauth_grants (id) ON DELETE CASCADE
);
CREATE INDEX oauth_codes_owner ON oauth_codes (owner_email);

CREATE TABLE oauth_tokens (
    id         INTEGER PRIMARY KEY,
    grant_id   INTEGER NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    expires_at TEXT NOT NULL,
    used_at    TEXT
);
CREATE INDEX oauth_tokens_grant ON oauth_tokens (grant_id);

-- +goose Down
DROP TABLE oauth_tokens;
DROP TABLE oauth_codes;
DROP TABLE oauth_grants;
DROP TABLE oauth_clients;
