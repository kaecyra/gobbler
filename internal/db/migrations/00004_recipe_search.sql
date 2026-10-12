-- +goose Up
-- Full-text search over recipes (ADR-00002): one FTS5 row per recipe, rowid =
-- recipes.id, over the name, description, ingredient text (the catalog name and
-- the line as written), tags and notes. SQLite triggers rebuild a recipe's row
-- whenever one of those changes, so every writer keeps the index in step without
-- calling it; the store package only reads it (internal/store/search.go).
--
-- A cascade delete of a recipe's children runs the child triggers after the
-- recipes row is gone, where the rebuild selects nothing: the recipes delete
-- trigger is what removes the row.

CREATE VIRTUAL TABLE recipe_search USING fts5 (
    name, description, ingredients, tags, notes,
    tokenize = 'unicode61 remove_diacritics 2'
);

-- What is indexed for a recipe, defined once: every trigger and the backfill
-- insert a row of this view, so a change to the indexed text is made here only.
-- Aliases, not a column list: sqlc names view columns from the SELECT.
CREATE VIEW recipe_search_source AS
SELECT r.id AS id, r.name AS name, r.description AS description,
       COALESCE((SELECT group_concat(COALESCE(i.name, '') || ' ' || l.raw, char(10))
                   FROM components c
                   JOIN ingredient_lines l ON l.component_id = c.id
                   LEFT JOIN ingredients i ON i.id = l.ingredient_id
                  WHERE c.recipe_id = r.id), '') AS ingredients,
       COALESCE((SELECT group_concat(tag, char(10)) FROM recipe_tags WHERE recipe_id = r.id), '') AS tags,
       COALESCE((SELECT group_concat(body, char(10)) FROM notes WHERE recipe_id = r.id), '') AS notes
  FROM recipes r;

-- +goose StatementBegin
CREATE TRIGGER recipe_search_recipes_ai AFTER INSERT ON recipes
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_recipes_au AFTER UPDATE OF name, description ON recipes
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_tags_ai AFTER INSERT ON recipe_tags
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_tags_ad AFTER DELETE ON recipe_tags
BEGIN
    DELETE FROM recipe_search WHERE rowid = OLD.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = OLD.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_components_ai AFTER INSERT ON components
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_components_ad AFTER DELETE ON components
BEGIN
    DELETE FROM recipe_search WHERE rowid = OLD.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = OLD.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_notes_ai AFTER INSERT ON notes
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_notes_ad AFTER DELETE ON notes
BEGIN
    DELETE FROM recipe_search WHERE rowid = OLD.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = OLD.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_tags_au AFTER UPDATE ON recipe_tags
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_notes_au AFTER UPDATE ON notes
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_components_au AFTER UPDATE ON components
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_lines_ai AFTER INSERT ON ingredient_lines
BEGIN
    DELETE FROM recipe_search WHERE rowid = (SELECT recipe_id FROM components WHERE id = NEW.component_id);
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = (SELECT recipe_id FROM components WHERE id = NEW.component_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_lines_au AFTER UPDATE ON ingredient_lines
BEGIN
    DELETE FROM recipe_search WHERE rowid = (SELECT recipe_id FROM components WHERE id = NEW.component_id);
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = (SELECT recipe_id FROM components WHERE id = NEW.component_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_lines_ad AFTER DELETE ON ingredient_lines
BEGIN
    DELETE FROM recipe_search WHERE rowid = (SELECT recipe_id FROM components WHERE id = OLD.component_id);
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source WHERE id = (SELECT recipe_id FROM components WHERE id = OLD.component_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_recipes_ad AFTER DELETE ON recipes
BEGIN
    DELETE FROM recipe_search WHERE rowid = OLD.id;
END;
-- +goose StatementEnd

-- A renamed catalog ingredient changes the text of every recipe that uses it.
-- +goose StatementBegin
CREATE TRIGGER recipe_search_ingredients_au AFTER UPDATE OF name ON ingredients
BEGIN
    DELETE FROM recipe_search WHERE rowid IN
        (SELECT c.recipe_id FROM ingredient_lines l JOIN components c ON c.id = l.component_id
          WHERE l.ingredient_id = NEW.id);
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
    SELECT * FROM recipe_search_source
     WHERE id IN (SELECT c.recipe_id FROM ingredient_lines l JOIN components c ON c.id = l.component_id
                   WHERE l.ingredient_id = NEW.id);
END;
-- +goose StatementEnd

-- Recipes that exist before this migration are indexed here.
INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes)
SELECT * FROM recipe_search_source;

-- +goose Down
DROP TRIGGER recipe_search_recipes_ai;
DROP TRIGGER recipe_search_recipes_au;
DROP TRIGGER recipe_search_tags_ai;
DROP TRIGGER recipe_search_tags_ad;
DROP TRIGGER recipe_search_components_ai;
DROP TRIGGER recipe_search_components_ad;
DROP TRIGGER recipe_search_notes_ai;
DROP TRIGGER recipe_search_notes_ad;
DROP TRIGGER recipe_search_tags_au;
DROP TRIGGER recipe_search_notes_au;
DROP TRIGGER recipe_search_components_au;
DROP TRIGGER recipe_search_lines_ai;
DROP TRIGGER recipe_search_lines_au;
DROP TRIGGER recipe_search_lines_ad;
DROP TRIGGER recipe_search_recipes_ad;
DROP TRIGGER recipe_search_ingredients_au;
DROP VIEW recipe_search_source;
DROP TABLE recipe_search;
