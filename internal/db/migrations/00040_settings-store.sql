-- +goose Up
-- Integration settings (ADR-00057): the mail and LLM settings an operator
-- edits on the Deployment page, per deployment and not per household. A row is
-- addressed by integration and field name. secret = 1 marks a sealed value:
-- value then holds the 12-byte nonce followed by the AES-256-GCM ciphertext
-- and tag, with integration and field as associated data, so a value copied
-- into another row fails to open. secret = 0 holds the setting's text as is.
-- Timestamps are RFC 3339 UTC text. The settings package owns the SQL
-- (internal/settings).
CREATE TABLE integration_settings (
    integration TEXT NOT NULL CHECK (integration <> ''),
    field       TEXT NOT NULL CHECK (field <> ''),
    secret      INTEGER NOT NULL CHECK (secret IN (0, 1)),
    value       BLOB NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (integration, field)
) WITHOUT ROWID;

-- +goose Down
DROP TABLE integration_settings;
