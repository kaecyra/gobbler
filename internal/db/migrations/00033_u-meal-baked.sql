-- +goose Up
-- A seventh meal type, baked, for breads, rolls, scones and buns (ADR-00022).
-- SQLite cannot alter a CHECK, and rebuilding recipes would mean rebuilding
-- every table that references it, so the column makes a round trip: its values
-- wait in a temp table while it is dropped and added again with the wider
-- CHECK. No index, view or trigger names meal_type, so the drop touches
-- nothing else. The column moves to the end of the table; every query names
-- its columns.
CREATE TEMP TABLE recipe_meal_type_keep AS SELECT id, meal_type FROM recipes;

ALTER TABLE recipes DROP COLUMN meal_type;
ALTER TABLE recipes ADD COLUMN meal_type TEXT NOT NULL DEFAULT ''
    CHECK (meal_type IN ('', 'breakfast', 'main', 'side', 'baked', 'dessert', 'snack', 'drink'));

UPDATE recipes SET meal_type = (SELECT meal_type FROM recipe_meal_type_keep WHERE id = recipes.id);

DROP TABLE recipe_meal_type_keep;

-- +goose Down
-- A baked recipe has no meal type in the old set, so it goes back to unset
-- (''). Every other value is kept.
CREATE TEMP TABLE recipe_meal_type_keep AS
SELECT id, CASE WHEN meal_type = 'baked' THEN '' ELSE meal_type END AS meal_type FROM recipes;

ALTER TABLE recipes DROP COLUMN meal_type;
ALTER TABLE recipes ADD COLUMN meal_type TEXT NOT NULL DEFAULT ''
    CHECK (meal_type IN ('', 'breakfast', 'main', 'side', 'dessert', 'snack', 'drink'));

UPDATE recipes SET meal_type = (SELECT meal_type FROM recipe_meal_type_keep WHERE id = recipes.id);

DROP TABLE recipe_meal_type_keep;
