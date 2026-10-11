-- +goose Up
-- Recipes (ADR-00004): the recipe document and its components, ingredient lines,
-- qualifiers, equipment links, steps and step dependencies, plus notes, made-it
-- entries, revisions and immutable source snapshots. The store package owns the
-- SQL (internal/store).
--
-- Quantities are one TEXT column: NULL for none, "n/d" for an exact value and
-- "n/d..n/d" for a range, with n/d the exact reduced fraction. Durations are
-- integer nanoseconds. Timestamps are RFC 3339 UTC text. Child rows carry a
-- position column that keeps their order.
--
-- Every table that references ingredients(id) uses a plain foreign key (no
-- cascade) and has a catalog.OnIngredientMerge hook registered by the store.

CREATE TABLE recipes (
    id                    INTEGER PRIMARY KEY,
    name                  TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    source_url            TEXT NOT NULL DEFAULT '',
    author                TEXT NOT NULL DEFAULT '',
    yield_qty             TEXT,
    yield_unit            TEXT NOT NULL DEFAULT '',
    servings              TEXT,
    calories_per_serving  INTEGER CHECK (calories_per_serving IS NULL OR calories_per_serving >= 0),
    rating                INTEGER,
    prep_ns               INTEGER NOT NULL DEFAULT 0 CHECK (prep_ns >= 0),
    cook_ns               INTEGER NOT NULL DEFAULT 0 CHECK (cook_ns >= 0),
    total_ns              INTEGER NOT NULL DEFAULT 0 CHECK (total_ns >= 0),
    -- version counts saves: 1 on create, +1 on every edit or restore. The
    -- revision with version N holds the document as it was at version N.
    version               INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    -- A clone records where it came from; the origin recipe may be deleted later.
    cloned_from_recipe_id INTEGER REFERENCES recipes (id) ON DELETE SET NULL,
    cloned_from_version   INTEGER,
    created_at            TEXT NOT NULL,
    created_by            TEXT NOT NULL CHECK (created_by <> ''),
    updated_at            TEXT NOT NULL,
    updated_by            TEXT NOT NULL CHECK (updated_by <> '')
);
CREATE INDEX recipes_cloned_from ON recipes (cloned_from_recipe_id);

CREATE TABLE recipe_tags (
    recipe_id INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    tag       TEXT NOT NULL,
    PRIMARY KEY (recipe_id, position)
);

CREATE TABLE recipe_special_times (
    recipe_id   INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    label       TEXT NOT NULL,
    duration_ns INTEGER NOT NULL CHECK (duration_ns >= 0),
    PRIMARY KEY (recipe_id, position)
);

CREATE TABLE components (
    id        INTEGER PRIMARY KEY,
    recipe_id INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    name      TEXT NOT NULL DEFAULT '',
    UNIQUE (recipe_id, position)
);

CREATE TABLE ingredient_lines (
    id            INTEGER PRIMARY KEY,
    component_id  INTEGER NOT NULL REFERENCES components (id) ON DELETE CASCADE,
    position      INTEGER NOT NULL,
    qty           TEXT,
    unit_id       INTEGER REFERENCES units (id),
    has_package   INTEGER NOT NULL DEFAULT 0 CHECK (has_package IN (0, 1)),
    package_qty   TEXT,
    package_unit_id INTEGER REFERENCES units (id),
    -- NULL means unresolved (recipe.IngredientID zero).
    ingredient_id INTEGER REFERENCES ingredients (id),
    optional      INTEGER NOT NULL DEFAULT 0 CHECK (optional IN (0, 1)),
    rule          TEXT NOT NULL CHECK (rule IN ('linear', 'fixed', 'to_taste')),
    raw           TEXT NOT NULL DEFAULT '',
    UNIQUE (component_id, position)
);
CREATE INDEX ingredient_lines_ingredient_id ON ingredient_lines (ingredient_id);

CREATE TABLE line_qualifiers (
    line_id  INTEGER NOT NULL REFERENCES ingredient_lines (id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    kind     TEXT NOT NULL CHECK (kind IN ('preparation', 'state', 'form', 'note')),
    text     TEXT NOT NULL CHECK (text <> ''),
    PRIMARY KEY (line_id, position)
);

CREATE TABLE steps (
    id           INTEGER PRIMARY KEY,
    recipe_id    INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    component_id INTEGER NOT NULL REFERENCES components (id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    -- step_key is recipe.StepID: the name dependencies use, unique per recipe.
    step_key     TEXT NOT NULL CHECK (step_key <> ''),
    text         TEXT NOT NULL DEFAULT '',
    duration_ns  INTEGER NOT NULL DEFAULT 0 CHECK (duration_ns >= 0),
    passive      INTEGER NOT NULL DEFAULT 0 CHECK (passive IN (0, 1)),
    UNIQUE (component_id, position),
    UNIQUE (recipe_id, step_key)
);

CREATE TABLE step_dependencies (
    step_id            INTEGER NOT NULL REFERENCES steps (id) ON DELETE CASCADE,
    position           INTEGER NOT NULL,
    depends_on_step_id INTEGER NOT NULL REFERENCES steps (id) ON DELETE CASCADE,
    PRIMARY KEY (step_id, position),
    CHECK (step_id <> depends_on_step_id)
);
CREATE INDEX step_dependencies_depends_on ON step_dependencies (depends_on_step_id);

CREATE TABLE recipe_equipment (
    id           INTEGER PRIMARY KEY,
    recipe_id    INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    equipment_id INTEGER NOT NULL REFERENCES equipment (id),
    qty          TEXT,
    UNIQUE (recipe_id, position)
);
CREATE INDEX recipe_equipment_equipment_id ON recipe_equipment (equipment_id);

CREATE TABLE recipe_equipment_steps (
    recipe_equipment_id INTEGER NOT NULL REFERENCES recipe_equipment (id) ON DELETE CASCADE,
    position            INTEGER NOT NULL,
    step_id             INTEGER NOT NULL REFERENCES steps (id) ON DELETE CASCADE,
    PRIMARY KEY (recipe_equipment_id, position)
);

CREATE TABLE notes (
    id         INTEGER PRIMARY KEY,
    recipe_id  INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    body       TEXT NOT NULL CHECK (body <> ''),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (created_by <> '')
);
CREATE INDEX notes_recipe_id ON notes (recipe_id);

CREATE TABLE made_it (
    id         INTEGER PRIMARY KEY,
    recipe_id  INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    made_on    TEXT NOT NULL,
    rating     INTEGER,
    comment    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (created_by <> '')
);
CREATE INDEX made_it_recipe_id ON made_it (recipe_id);

-- A revision holds the full recipe document (JSON) as it was at `version`,
-- written in the same transaction as the edit that replaced it. created_at and
-- created_by are the time and author of that edit. change_kind says what the
-- edit was; a restore names the version it restored.
CREATE TABLE revisions (
    id               INTEGER PRIMARY KEY,
    recipe_id        INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    version          INTEGER NOT NULL CHECK (version >= 1),
    document         TEXT NOT NULL,
    change_kind      TEXT NOT NULL CHECK (change_kind IN ('edit', 'restore')),
    restored_version INTEGER,
    created_at       TEXT NOT NULL,
    created_by       TEXT NOT NULL CHECK (created_by <> ''),
    UNIQUE (recipe_id, version)
);

-- Ingredient ids that appear inside revision documents. The document is opaque
-- JSON; this table is what lets the ingredient merge find and rewrite them.
CREATE TABLE revision_ingredients (
    revision_id   INTEGER NOT NULL REFERENCES revisions (id) ON DELETE CASCADE,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id),
    PRIMARY KEY (revision_id, ingredient_id)
);
CREATE INDEX revision_ingredients_ingredient_id ON revision_ingredients (ingredient_id);

-- The ingested input, stored once per kind and never altered (ADR-00004
-- obligation 4). The triggers hold the line below the Go API: no update at all,
-- no delete except when the recipe itself is being deleted, and no insert over
-- an existing (recipe, kind): INSERT OR REPLACE removes the old row without
-- firing delete triggers, so the insert is what has to be refused.
CREATE TABLE source_snapshots (
    id         INTEGER PRIMARY KEY,
    recipe_id  INTEGER NOT NULL REFERENCES recipes (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('html', 'text', 'ocr', 'llm_response')),
    content    BLOB NOT NULL,
    sha256     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (created_by <> ''),
    UNIQUE (recipe_id, kind)
);

-- +goose StatementBegin
CREATE TRIGGER source_snapshots_no_update BEFORE UPDATE ON source_snapshots
BEGIN
    SELECT RAISE(ABORT, 'source snapshots are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER source_snapshots_no_delete BEFORE DELETE ON source_snapshots
WHEN EXISTS (SELECT 1 FROM recipes WHERE id = OLD.recipe_id)
BEGIN
    SELECT RAISE(ABORT, 'source snapshots are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER source_snapshots_no_replace BEFORE INSERT ON source_snapshots
WHEN EXISTS (SELECT 1 FROM source_snapshots WHERE recipe_id = NEW.recipe_id AND kind = NEW.kind)
BEGIN
    SELECT RAISE(ABORT, 'source snapshots are immutable');
END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER source_snapshots_no_replace;
DROP TRIGGER source_snapshots_no_delete;
DROP TRIGGER source_snapshots_no_update;
DROP TABLE source_snapshots;
DROP TABLE revision_ingredients;
DROP TABLE revisions;
DROP TABLE made_it;
DROP TABLE notes;
DROP TABLE recipe_equipment_steps;
DROP TABLE recipe_equipment;
DROP TABLE step_dependencies;
DROP TABLE steps;
DROP TABLE line_qualifiers;
DROP TABLE ingredient_lines;
DROP TABLE components;
DROP TABLE recipe_special_times;
DROP TABLE recipe_tags;
DROP TABLE recipes;
