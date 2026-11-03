-- +goose Up
-- The write log of API keys (ADR-00031 obligation 5, ADR-00034): one row for
-- each write an MCP tool made with a key, so a recipe draft or a shopping-list
-- change can be traced to the credential that made it. key_id is deliberately
-- not a foreign key: revoking a key deletes it (ADR-00031), and the record of
-- what it did must outlive it. household_id is the household the write acted
-- on. tool is the MCP tool's name and target names what it changed. Timestamps
-- are RFC 3339 UTC text.
CREATE TABLE api_key_writes (
    id           INTEGER PRIMARY KEY,
    key_id       INTEGER NOT NULL,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    tool         TEXT NOT NULL CHECK (tool <> ''),
    target       TEXT NOT NULL,
    created_at   TEXT NOT NULL
);
CREATE INDEX api_key_writes_key ON api_key_writes (key_id);
CREATE INDEX api_key_writes_household ON api_key_writes (household_id);

-- +goose Down
DROP TABLE api_key_writes;
