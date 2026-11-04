-- +goose Up
-- Proposals and stable element keys (ADR-00035).
--
-- Keys. Every step and ingredient line carries a key, unique within its recipe
-- for the recipe's whole life: "stp-" or "lin-" and 8 random lowercase Crockford
-- base32 characters. Every save rewrites a recipe's child rows, so row ids
-- cannot name an element across versions; a key can. recipe_keys is the record
-- of every key a recipe has ever held, which is what lets the store promise it
-- never gives a removed element's key to a new one.
--
-- Existing steps (whose step_key was an ingest counter or an editor id) and
-- lines are given keys in the new format here. Step dependencies and equipment
-- links refer to step rows, not to keys, so rewriting step_key breaks nothing.
-- Revisions written before this keep the old step ids in their documents.

CREATE TABLE recipe_keys (
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    key          TEXT NOT NULL CHECK (key <> ''),
    PRIMARY KEY (recipe_id, key),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);

ALTER TABLE ingredient_lines ADD COLUMN line_key TEXT NOT NULL DEFAULT '';

-- 8 random characters of the Crockford base32 alphabet (no i, l, o, u),
-- lowercase. random() is evaluated once per call, so each character differs.
UPDATE steps SET step_key = 'stp-'
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1);

UPDATE ingredient_lines SET line_key = 'lin-'
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1)
    || substr('0123456789abcdefghjkmnpqrstvwxyz', 1 + abs(random()) % 32, 1);

INSERT INTO recipe_keys (household_id, recipe_id, key)
SELECT household_id, recipe_id, step_key FROM steps;
INSERT INTO recipe_keys (household_id, recipe_id, key)
SELECT l.household_id, c.recipe_id, l.line_key
FROM ingredient_lines l JOIN components c ON c.id = l.component_id;

-- Source snapshots may have no recipe. A draft's source is written when the
-- draft is made and attached to the recipe it becomes on confirm (ADR-00035).
-- SQLite cannot drop NOT NULL, so the table is rebuilt; rows keep their ids.
-- The composite foreign key is not enforced for a NULL recipe_id, and
-- UNIQUE (recipe_id, kind) treats NULLs as distinct, so unattached snapshots
-- neither need a recipe nor collide.
CREATE TABLE source_snapshots_new (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id      INTEGER,
    kind           TEXT NOT NULL CHECK (kind IN ('html', 'text', 'ocr', 'llm_response')),
    content        BLOB NOT NULL,
    sha256         TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    created_by     TEXT NOT NULL CHECK (created_by <> ''),
    parser_version TEXT NOT NULL DEFAULT '',
    UNIQUE (recipe_id, kind),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);
INSERT INTO source_snapshots_new (id, household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version)
SELECT id, household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots;
DROP TABLE source_snapshots;
ALTER TABLE source_snapshots_new RENAME TO source_snapshots;

-- A proposal is a whole document that is not the current recipe, awaiting a
-- decision: kind 'draft' is an ingestion draft of a new recipe (recipe_id and
-- base_version NULL), kind 'edit' is a proposed new version of recipe_id made
-- against base_version. document is the draft's JSON or the revision document
-- format; nothing outside proposal review and display reads it, and the
-- recipe tables never see it.
--
-- token is the unguessable name the review screen uses. result_recipe_id is the
-- recipe a confirmed draft created. Deleting a recipe deletes the proposals
-- about it and the ones that made it with its history (ADR-00015).
-- snapshot_id is a draft's source snapshot; it is set NULL, not cascaded, so
-- pruning a snapshot never silently deletes the row that explains it.
--
-- Provenance: proposed_by is the person; credential_* is the snapshot of the
-- credential that made it, taken as the write logs take it (migration 00026),
-- because a revoked credential's id is given to a later one; tool is the MCP
-- tool; reason is what an agent said. state_at is when the state last changed.
-- At most one edit proposal per recipe is pending.
CREATE TABLE proposals (
    id                    INTEGER PRIMARY KEY,
    household_id          INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    token                 TEXT NOT NULL UNIQUE CHECK (token <> ''),
    kind                  TEXT NOT NULL CHECK (kind IN ('draft', 'edit')),
    state                 TEXT NOT NULL CHECK (state IN ('pending', 'accepted', 'rejected', 'superseded', 'expired')),
    recipe_id             INTEGER,
    base_version          INTEGER CHECK (base_version IS NULL OR base_version >= 1),
    result_recipe_id      INTEGER,
    snapshot_id           INTEGER REFERENCES source_snapshots (id) ON DELETE SET NULL,
    document              TEXT NOT NULL,
    reason                TEXT NOT NULL DEFAULT '',
    proposed_by           TEXT NOT NULL CHECK (proposed_by <> ''),
    credential_kind       TEXT NOT NULL DEFAULT '',
    credential_id         INTEGER NOT NULL DEFAULT 0,
    credential_ref        TEXT NOT NULL DEFAULT '',
    credential_owner      TEXT NOT NULL DEFAULT '',
    credential_created_at TEXT NOT NULL DEFAULT '',
    tool                  TEXT NOT NULL DEFAULT '',
    created_at            TEXT NOT NULL,
    state_at              TEXT NOT NULL,
    CHECK ((kind = 'draft' AND recipe_id IS NULL AND base_version IS NULL)
        OR (kind = 'edit' AND recipe_id IS NOT NULL AND base_version IS NOT NULL)),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, result_recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);
CREATE INDEX proposals_household_state ON proposals (household_id, kind, state);
CREATE INDEX proposals_recipe ON proposals (recipe_id);
CREATE UNIQUE INDEX proposals_one_pending_edit ON proposals (recipe_id) WHERE kind = 'edit' AND state = 'pending';

-- The ingredient ids inside proposal documents, with a foreign key to the
-- catalog as revision_ingredients has: an ingredient merge that forgets to
-- re-point a proposal fails on this key instead of leaving a dangling id.
CREATE TABLE proposal_ingredients (
    household_id  INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    proposal_id   INTEGER NOT NULL,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id),
    PRIMARY KEY (proposal_id, ingredient_id),
    FOREIGN KEY (household_id, proposal_id) REFERENCES proposals (household_id, id) ON DELETE CASCADE
);
CREATE INDEX proposal_ingredients_ingredient_id ON proposal_ingredients (ingredient_id);

-- +goose Down
DROP TABLE proposal_ingredients;
DROP TABLE proposals;
DROP TABLE recipe_keys;
ALTER TABLE ingredient_lines DROP COLUMN line_key;
-- Snapshots with no recipe cannot exist in the old shape.
DELETE FROM source_snapshots WHERE recipe_id IS NULL;
CREATE TABLE source_snapshots_old (
    id             INTEGER PRIMARY KEY,
    household_id   INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id      INTEGER NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('html', 'text', 'ocr', 'llm_response')),
    content        BLOB NOT NULL,
    sha256         TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    created_by     TEXT NOT NULL CHECK (created_by <> ''),
    parser_version TEXT NOT NULL DEFAULT '',
    UNIQUE (recipe_id, kind),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id) ON DELETE CASCADE
);
INSERT INTO source_snapshots_old (id, household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version)
SELECT id, household_id, recipe_id, kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots;
DROP TABLE source_snapshots;
ALTER TABLE source_snapshots_old RENAME TO source_snapshots;
