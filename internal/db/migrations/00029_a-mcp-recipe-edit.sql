-- +goose Up
-- A revision can say it was written by accepting a proposal (ADR-00035,
-- History): change_kind gains 'proposal', and proposal_id names the proposal.
-- Provenance lives once, on the proposal; the revision only refers to it.
--
-- SQLite cannot alter a CHECK or add a foreign-key column with a constraint,
-- so revisions is rebuilt as 00028 rebuilt steps, with revision_ingredients,
-- which references it and would be emptied by the cascade when the old table is
-- dropped. Every row keeps its id and is copied unchanged.
--
-- The foreign key to proposals has no ON DELETE action: nothing deletes an
-- accepted proposal except its recipe (ADR-00035), and the recipe's delete
-- removes the revisions in the same statement. A proposal a revision refers to
-- cannot be removed on its own.
CREATE TABLE revisions_new (
    id               INTEGER PRIMARY KEY,
    household_id     INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id        INTEGER NOT NULL,
    version          INTEGER NOT NULL CHECK (version >= 1),
    document         TEXT NOT NULL,
    change_kind      TEXT NOT NULL CHECK (change_kind IN ('edit', 'restore', 'proposal')),
    restored_version INTEGER,
    proposal_id      INTEGER,
    created_at       TEXT NOT NULL,
    created_by       TEXT NOT NULL CHECK (created_by <> ''),
    CHECK ((change_kind = 'proposal') = (proposal_id IS NOT NULL)),
    UNIQUE (recipe_id, version),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, proposal_id) REFERENCES proposals (household_id, id)
);
INSERT INTO revisions_new (id, household_id, recipe_id, version, document, change_kind, restored_version, created_at, created_by)
SELECT id, household_id, recipe_id, version, document, change_kind, restored_version, created_at, created_by
FROM revisions;

CREATE TABLE revision_ingredients_new (
    household_id  INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    revision_id   INTEGER NOT NULL,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id),
    PRIMARY KEY (revision_id, ingredient_id),
    FOREIGN KEY (household_id, revision_id) REFERENCES revisions_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO revision_ingredients_new (household_id, revision_id, ingredient_id)
SELECT household_id, revision_id, ingredient_id FROM revision_ingredients;

DROP TABLE revision_ingredients;
DROP TABLE revisions;

ALTER TABLE revisions_new RENAME TO revisions;
ALTER TABLE revision_ingredients_new RENAME TO revision_ingredients;

CREATE INDEX revision_ingredients_ingredient_id ON revision_ingredients (ingredient_id);
CREATE INDEX revisions_proposal ON revisions (proposal_id);

-- +goose Down
-- A revision written by a proposal goes back to being an ordinary edit.
CREATE TABLE revisions_old (
    id               INTEGER PRIMARY KEY,
    household_id     INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id        INTEGER NOT NULL,
    version          INTEGER NOT NULL CHECK (version >= 1),
    document         TEXT NOT NULL,
    change_kind      TEXT NOT NULL CHECK (change_kind IN ('edit', 'restore')),
    restored_version INTEGER,
    created_at       TEXT NOT NULL,
    created_by       TEXT NOT NULL CHECK (created_by <> ''),
    UNIQUE (recipe_id, version),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);
INSERT INTO revisions_old (id, household_id, recipe_id, version, document, change_kind, restored_version, created_at, created_by)
SELECT id, household_id, recipe_id, version, document, CASE WHEN change_kind = 'proposal' THEN 'edit' ELSE change_kind END, restored_version, created_at, created_by
FROM revisions;

CREATE TABLE revision_ingredients_old (
    household_id  INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    revision_id   INTEGER NOT NULL,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id),
    PRIMARY KEY (revision_id, ingredient_id),
    FOREIGN KEY (household_id, revision_id) REFERENCES revisions_old (household_id, id) ON DELETE CASCADE
);
INSERT INTO revision_ingredients_old (household_id, revision_id, ingredient_id)
SELECT household_id, revision_id, ingredient_id FROM revision_ingredients;

DROP TABLE revision_ingredients;
DROP TABLE revisions;

ALTER TABLE revisions_old RENAME TO revisions;
ALTER TABLE revision_ingredients_old RENAME TO revision_ingredients;

CREATE INDEX revision_ingredients_ingredient_id ON revision_ingredients (ingredient_id);
