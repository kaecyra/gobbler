-- +goose Up
-- The recipe author's notes (ADR-00041): an ordered list on the recipe, each
-- with an optional title and a required text. Recipe validation holds the
-- one-paragraph, 400-character limit for every writer; the table keeps only
-- that the text is not empty. The list goes with its recipe, and a revision
-- carries it in its document.
CREATE TABLE recipe_author_notes (
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    position     INTEGER NOT NULL CHECK (position >= 0),
    title        TEXT NOT NULL DEFAULT '',
    text         TEXT NOT NULL CHECK (text <> ''),
    PRIMARY KEY (recipe_id, position),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);
CREATE INDEX recipe_author_notes_household ON recipe_author_notes (household_id);

-- +goose Down
DROP TABLE recipe_author_notes;
