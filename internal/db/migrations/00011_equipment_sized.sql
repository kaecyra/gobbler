-- +goose Up
-- Catalog equipment gains a hand-set sized flag (ADR-00019): pans, tins, dishes
-- and pots the operator marked during catalog review. A scaled recipe view shows
-- its larger-pan hint on sized equipment only (ADR-00005, Scaling). Default off,
-- so no existing row is sized until the operator says so.
ALTER TABLE equipment ADD COLUMN sized INTEGER NOT NULL DEFAULT 0 CHECK (sized IN (0, 1));

-- +goose Down
ALTER TABLE equipment DROP COLUMN sized;
