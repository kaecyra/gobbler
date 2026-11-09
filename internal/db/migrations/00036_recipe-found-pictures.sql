-- +goose Up
-- Pictures offered for a saved recipe on its edit page (ADR-00051): the
-- candidates a search of its source page found, and a picture uploaded there,
-- kept as objects under recipes/<recipe_id>/found/<offer>.webp until the edit
-- form is saved or FoundLifetime passes. offer is the picture's opaque ID, a
-- random token never reused: the edit form chooses by it, so a later search
-- cannot put a different picture behind a choice already on the page. kind
-- 'found' rows are replaced as a set by the next search; the one 'upload' row
-- by the next upload. They go with their recipe and household; the objects
-- are deleted by the picture package, which collects the keys before the
-- delete. width and height are those of the stored (fitted) picture.
-- internal/picture owns the SQL.
CREATE TABLE recipe_found_pictures (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    offer        TEXT NOT NULL UNIQUE CHECK (offer <> ''),
    kind         TEXT NOT NULL CHECK (kind IN ('found', 'upload')),
    source_url   TEXT NOT NULL,
    key          TEXT NOT NULL CHECK (key <> ''),
    width        INTEGER NOT NULL CHECK (width >= 1),
    height       INTEGER NOT NULL CHECK (height >= 1),
    found_at     TEXT NOT NULL,
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);
CREATE INDEX recipe_found_pictures_recipe ON recipe_found_pictures (recipe_id);
CREATE INDEX recipe_found_pictures_household ON recipe_found_pictures (household_id);
CREATE INDEX recipe_found_pictures_found_at ON recipe_found_pictures (found_at);

-- +goose Down
DROP TABLE recipe_found_pictures;
