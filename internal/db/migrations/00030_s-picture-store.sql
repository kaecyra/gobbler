-- +goose Up
-- Recipe pictures (ADR-00039, Storing). The picture's object key is a row of
-- its own, outside the recipe document: a picture change is not a revision and
-- restoring a revision leaves the picture as it is. The row goes with its
-- recipe. internal/picture owns the SQL.
--
-- draft_candidates are the pictures fetched for a draft waiting for review,
-- kept as objects under drafts/<proposal_id>/<position>.webp until the person
-- chooses one or the proposal ends. They go with their proposal; the objects
-- are deleted by the picture package, which collects the keys before the delete.
-- width and height are those of the stored (fitted) picture.
CREATE TABLE recipe_pictures (
    recipe_id    INTEGER NOT NULL PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    key          TEXT NOT NULL CHECK (key <> ''),
    updated_at   TEXT NOT NULL,
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);
CREATE INDEX recipe_pictures_household ON recipe_pictures (household_id);

CREATE TABLE draft_candidates (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    proposal_id  INTEGER NOT NULL,
    position     INTEGER NOT NULL CHECK (position >= 1),
    source_url   TEXT NOT NULL,
    key          TEXT NOT NULL CHECK (key <> ''),
    width        INTEGER NOT NULL CHECK (width >= 1),
    height       INTEGER NOT NULL CHECK (height >= 1),
    UNIQUE (proposal_id, position),
    FOREIGN KEY (household_id, proposal_id) REFERENCES proposals (household_id, id) ON DELETE CASCADE
);
CREATE INDEX draft_candidates_household ON draft_candidates (household_id);

-- +goose Down
DROP TABLE draft_candidates;
DROP TABLE recipe_pictures;
