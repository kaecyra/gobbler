-- +goose Up
-- A recipe's one cuisine, from a fixed list (ADR-00025, amending ADR-00004).
-- It is part of the recipe document, so a revision, a restore and a shared copy
-- carry it. Empty means "not set", which is every row stored before this
-- migration.
ALTER TABLE recipes ADD COLUMN cuisine TEXT NOT NULL DEFAULT ''
    CHECK (cuisine IN ('', 'american', 'british', 'caribbean', 'chinese', 'french', 'german', 'greek', 'indian', 'italian', 'japanese', 'korean', 'mediterranean', 'mexican', 'middle_eastern', 'spanish', 'thai', 'vietnamese', 'other'));

-- +goose Down
ALTER TABLE recipes DROP COLUMN cuisine;
