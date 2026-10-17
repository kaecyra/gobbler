-- +goose Up
-- The optional fields the reviewed mockup needs (ADR-00022, amending ADR-00004).
-- Each is part of the recipe document, so a revision, a restore and a shared
-- copy carry it, except the pin on a note, which belongs to the note.
--
-- Empty meal_type, empty stage_kind and a NULL spice_level all mean "not set",
-- which is every row stored before this migration. A spice level of 0 is a
-- recipe that is not spicy, and is not the same as unset.
ALTER TABLE recipes ADD COLUMN meal_type TEXT NOT NULL DEFAULT ''
    CHECK (meal_type IN ('', 'breakfast', 'main', 'side', 'dessert', 'snack', 'drink'));
ALTER TABLE recipes ADD COLUMN spice_level INTEGER
    CHECK (spice_level IS NULL OR spice_level BETWEEN 0 AND 3);

ALTER TABLE steps ADD COLUMN stage_kind TEXT NOT NULL DEFAULT ''
    CHECK (stage_kind IN ('', 'prep', 'rise', 'rest', 'steep', 'marinate', 'chill', 'bake', 'roast', 'simmer', 'braise', 'cook'));
ALTER TABLE steps ADD COLUMN title TEXT NOT NULL DEFAULT '';

-- A pinned note is a tip, shown in the recipe view.
ALTER TABLE notes ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1));

-- +goose Down
ALTER TABLE notes DROP COLUMN pinned;
ALTER TABLE steps DROP COLUMN title;
ALTER TABLE steps DROP COLUMN stage_kind;
ALTER TABLE recipes DROP COLUMN spice_level;
ALTER TABLE recipes DROP COLUMN meal_type;
