-- +goose Up
-- The writes an OAuth grant makes through the MCP endpoint (ADR-00034, which
-- applies ADR-00031 obligation 5 to tokens), and a fix to the key write log.
--
-- Both credential tables use a bare INTEGER PRIMARY KEY and revoking deletes the
-- row, so SQLite hands the freed id to the next credential. A log keyed on the
-- id alone would then show a revoked credential's writes under a later one. So
-- every write also records a snapshot of the credential that cannot recur: for
-- a grant its client, owner and creation time; for a key its prefix, owner and
-- creation time. Reading a log matches the id and the snapshot.
--
-- oauth_grant_writes is api_key_writes for grants: a separate table, since a
-- grant id and a key id are different numbers. grant_id has no foreign key,
-- because revoking a grant deletes it and the record of what it wrote must
-- outlive that. The household cascade matches api_key_writes: deleting a
-- household deletes its history.
--
-- api_key_writes gains the snapshot columns. key_owner is the owner's email, or
-- 'household:<id>' for a household key. Rows written before this migration have
-- '' and match no key: the id alone cannot say which credential made them.

CREATE TABLE oauth_grant_writes (
    id               INTEGER PRIMARY KEY,
    grant_id         INTEGER NOT NULL,
    grant_client_id  TEXT NOT NULL CHECK (grant_client_id <> ''),
    grant_owner      TEXT NOT NULL CHECK (grant_owner <> ''),
    grant_created_at TEXT NOT NULL CHECK (grant_created_at <> ''),
    household_id     INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    tool             TEXT NOT NULL CHECK (tool <> ''),
    target           TEXT NOT NULL,
    created_at       TEXT NOT NULL
);
CREATE INDEX oauth_grant_writes_grant ON oauth_grant_writes (grant_id);
CREATE INDEX oauth_grant_writes_household ON oauth_grant_writes (household_id);

ALTER TABLE api_key_writes ADD COLUMN key_prefix TEXT NOT NULL DEFAULT '';
ALTER TABLE api_key_writes ADD COLUMN key_owner TEXT NOT NULL DEFAULT '';
ALTER TABLE api_key_writes ADD COLUMN key_created_at TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE api_key_writes DROP COLUMN key_created_at;
ALTER TABLE api_key_writes DROP COLUMN key_owner;
ALTER TABLE api_key_writes DROP COLUMN key_prefix;
DROP TABLE oauth_grant_writes;
