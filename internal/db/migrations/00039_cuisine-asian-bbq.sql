-- +goose Up
-- Two broader cuisines, asian and bbq (ADR-00025, decision 1). SQLite cannot
-- alter a CHECK, and rebuilding recipes would mean rebuilding every table that
-- references it, so the column makes a round trip as in 00033: its values wait
-- in a temp table while it is dropped and added again with the wider CHECK. No
-- index, view or trigger names cuisine, so the drop touches nothing else. The
-- column moves to the end of the table; every query names its columns.
CREATE TEMP TABLE recipe_cuisine_keep AS SELECT id, cuisine FROM recipes;

ALTER TABLE recipes DROP COLUMN cuisine;
ALTER TABLE recipes ADD COLUMN cuisine TEXT NOT NULL DEFAULT ''
    CHECK (cuisine IN ('', 'american', 'asian', 'bbq', 'british', 'caribbean', 'chinese', 'french', 'german', 'greek', 'indian', 'italian', 'japanese', 'korean', 'mediterranean', 'mexican', 'middle_eastern', 'spanish', 'thai', 'vietnamese', 'other'));

UPDATE recipes SET cuisine = (SELECT cuisine FROM recipe_cuisine_keep WHERE id = recipes.id);

DROP TABLE recipe_cuisine_keep;

-- +goose Down
-- An asian or bbq recipe has no cuisine in the old set, so it goes back to
-- unset (''). Every other value is kept.
CREATE TEMP TABLE recipe_cuisine_keep AS
SELECT id, CASE WHEN cuisine IN ('asian', 'bbq') THEN '' ELSE cuisine END AS cuisine FROM recipes;

ALTER TABLE recipes DROP COLUMN cuisine;
ALTER TABLE recipes ADD COLUMN cuisine TEXT NOT NULL DEFAULT ''
    CHECK (cuisine IN ('', 'american', 'british', 'caribbean', 'chinese', 'french', 'german', 'greek', 'indian', 'italian', 'japanese', 'korean', 'mediterranean', 'mexican', 'middle_eastern', 'spanish', 'thai', 'vietnamese', 'other'));

UPDATE recipes SET cuisine = (SELECT cuisine FROM recipe_cuisine_keep WHERE id = recipes.id);

DROP TABLE recipe_cuisine_keep;
