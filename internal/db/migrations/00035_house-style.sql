-- +goose Up
-- A household's house style (ADR-00048): plain text that tells an agent how
-- the household shapes its recipes. Empty means none has been written. Any
-- member edits it on the household page; no MCP tool writes it. Its 16 KiB
-- bound is the household store's (household.MaxHouseStyleBytes), checked
-- before every write, as the store checks a household's name.
ALTER TABLE households ADD COLUMN house_style TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE households DROP COLUMN house_style;
