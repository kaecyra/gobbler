-- +goose Up
-- The ingest parser version that produced a recipe, stored with its source
-- snapshot (ADR-00004, ADR-00006). Ingestion is deterministic per parser
-- version, so with the snapshot and the version a recipe can be re-ingested and
-- compared. Empty means unknown: every snapshot stored before this migration,
-- and any added by hand. Written once with the snapshot, like its other columns.
ALTER TABLE source_snapshots ADD COLUMN parser_version TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE source_snapshots DROP COLUMN parser_version;
