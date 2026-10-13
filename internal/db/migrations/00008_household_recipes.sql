-- +goose Up
-- Recipes belong to a household (ADR-00018): household_id (NOT NULL, a foreign
-- key to households, ON DELETE CASCADE) on recipes and on every table below
-- them, and on the full-text index. The store package scopes every query by it.
--
-- SQLite cannot add a NOT NULL column that references another table, so each
-- table is rebuilt (create the new shape, copy, drop the old, rename). Rows
-- keep their ids. A table's household_id is tied to its parent's by a
-- composite foreign key (household_id, parent_id) onto the parent's
-- UNIQUE (household_id, id), so a child row can never name another
-- household's recipe, component, step or revision. cloned_from_recipe_id
-- cannot use one: ON DELETE SET NULL would also null household_id, so a
-- trigger keeps a clone in its origin's household instead.
--
-- No deployment holds data (ADR-00018), so there is nothing to move, but a
-- database that already has recipe rows must still migrate: they are assigned
-- to the household with the lowest id, and if there are recipe rows and no
-- household at all, the copy fails on NOT NULL recipes.household_id and the
-- migration aborts rather than inventing an owner.
--
-- Foreign keys are enforced while this runs, so shopping_entries (which
-- references recipes(id)) is emptied for the drop and refilled afterwards.

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

CREATE TEMP TABLE shopping_entries_keep AS SELECT id, list_id, recipe_id, multiplier FROM shopping_entries;
DELETE FROM shopping_entries;

CREATE TABLE recipes_new (
    id                    INTEGER PRIMARY KEY,
    household_id          INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
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
    version               INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    cloned_from_recipe_id INTEGER REFERENCES recipes_new (id) ON DELETE SET NULL,
    cloned_from_version   INTEGER,
    created_at            TEXT NOT NULL,
    created_by            TEXT NOT NULL CHECK (created_by <> ''),
    updated_at            TEXT NOT NULL,
    updated_by            TEXT NOT NULL CHECK (updated_by <> ''),
    UNIQUE (household_id, id)
);
INSERT INTO recipes_new (id, household_id, name, description, source_url, author, yield_qty, yield_unit, servings,
    calories_per_serving, rating, prep_ns, cook_ns, total_ns, version, cloned_from_recipe_id, cloned_from_version,
    created_at, created_by, updated_at, updated_by)
SELECT id, (SELECT min(id) FROM households), name, description, source_url, author, yield_qty, yield_unit, servings,
    calories_per_serving, rating, prep_ns, cook_ns, total_ns, version, cloned_from_recipe_id, cloned_from_version,
    created_at, created_by, updated_at, updated_by
FROM recipes;

CREATE TABLE recipe_tags_new (
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    position     INTEGER NOT NULL,
    tag          TEXT NOT NULL,
    PRIMARY KEY (recipe_id, position),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO recipe_tags_new (household_id, recipe_id, position, tag)
SELECT r.household_id, t.recipe_id, t.position, t.tag FROM recipe_tags t JOIN recipes_new r ON r.id = t.recipe_id;

CREATE TABLE recipe_special_times_new (
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    position     INTEGER NOT NULL,
    label        TEXT NOT NULL,
    duration_ns  INTEGER NOT NULL CHECK (duration_ns >= 0),
    PRIMARY KEY (recipe_id, position),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO recipe_special_times_new (household_id, recipe_id, position, label, duration_ns)
SELECT r.household_id, t.recipe_id, t.position, t.label, t.duration_ns
FROM recipe_special_times t JOIN recipes_new r ON r.id = t.recipe_id;

CREATE TABLE components_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    position     INTEGER NOT NULL,
    name         TEXT NOT NULL DEFAULT '',
    UNIQUE (recipe_id, position),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO components_new (id, household_id, recipe_id, position, name)
SELECT c.id, r.household_id, c.recipe_id, c.position, c.name FROM components c JOIN recipes_new r ON r.id = c.recipe_id;

CREATE TABLE ingredient_lines_new (
    id              INTEGER PRIMARY KEY,
    household_id    INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    component_id    INTEGER NOT NULL,
    position        INTEGER NOT NULL,
    qty             TEXT,
    unit_id         INTEGER REFERENCES units (id),
    has_package     INTEGER NOT NULL DEFAULT 0 CHECK (has_package IN (0, 1)),
    package_qty     TEXT,
    package_unit_id INTEGER REFERENCES units (id),
    -- NULL means unresolved (recipe.IngredientID zero).
    ingredient_id   INTEGER REFERENCES ingredients (id),
    optional        INTEGER NOT NULL DEFAULT 0 CHECK (optional IN (0, 1)),
    rule            TEXT NOT NULL CHECK (rule IN ('linear', 'fixed', 'to_taste')),
    raw             TEXT NOT NULL DEFAULT '',
    UNIQUE (component_id, position),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, component_id) REFERENCES components_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO ingredient_lines_new (id, household_id, component_id, position, qty, unit_id, has_package, package_qty,
    package_unit_id, ingredient_id, optional, rule, raw)
SELECT l.id, c.household_id, l.component_id, l.position, l.qty, l.unit_id, l.has_package, l.package_qty,
    l.package_unit_id, l.ingredient_id, l.optional, l.rule, l.raw
FROM ingredient_lines l JOIN components_new c ON c.id = l.component_id;

CREATE TABLE line_qualifiers_new (
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    line_id      INTEGER NOT NULL,
    position     INTEGER NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('preparation', 'state', 'form', 'note')),
    text         TEXT NOT NULL CHECK (text <> ''),
    PRIMARY KEY (line_id, position),
    FOREIGN KEY (household_id, line_id) REFERENCES ingredient_lines_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO line_qualifiers_new (household_id, line_id, position, kind, text)
SELECT l.household_id, q.line_id, q.position, q.kind, q.text
FROM line_qualifiers q JOIN ingredient_lines_new l ON l.id = q.line_id;

CREATE TABLE steps_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    component_id INTEGER NOT NULL,
    position     INTEGER NOT NULL,
    -- step_key is recipe.StepID: the name dependencies use, unique per recipe.
    step_key     TEXT NOT NULL CHECK (step_key <> ''),
    text         TEXT NOT NULL DEFAULT '',
    duration_ns  INTEGER NOT NULL DEFAULT 0 CHECK (duration_ns >= 0),
    passive      INTEGER NOT NULL DEFAULT 0 CHECK (passive IN (0, 1)),
    UNIQUE (component_id, position),
    UNIQUE (recipe_id, step_key),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes_new (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, component_id) REFERENCES components_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO steps_new (id, household_id, recipe_id, component_id, position, step_key, text, duration_ns, passive)
SELECT s.id, r.household_id, s.recipe_id, s.component_id, s.position, s.step_key, s.text, s.duration_ns, s.passive
FROM steps s JOIN recipes_new r ON r.id = s.recipe_id;

CREATE TABLE step_dependencies_new (
    household_id       INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    step_id            INTEGER NOT NULL,
    position           INTEGER NOT NULL,
    depends_on_step_id INTEGER NOT NULL,
    PRIMARY KEY (step_id, position),
    CHECK (step_id <> depends_on_step_id),
    FOREIGN KEY (household_id, step_id) REFERENCES steps_new (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, depends_on_step_id) REFERENCES steps_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO step_dependencies_new (household_id, step_id, position, depends_on_step_id)
SELECT s.household_id, d.step_id, d.position, d.depends_on_step_id
FROM step_dependencies d JOIN steps_new s ON s.id = d.step_id;

CREATE TABLE recipe_equipment_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    position     INTEGER NOT NULL,
    equipment_id INTEGER NOT NULL REFERENCES equipment (id),
    qty          TEXT,
    UNIQUE (recipe_id, position),
    UNIQUE (household_id, id),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO recipe_equipment_new (id, household_id, recipe_id, position, equipment_id, qty)
SELECT e.id, r.household_id, e.recipe_id, e.position, e.equipment_id, e.qty
FROM recipe_equipment e JOIN recipes_new r ON r.id = e.recipe_id;

CREATE TABLE recipe_equipment_steps_new (
    household_id        INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_equipment_id INTEGER NOT NULL,
    position            INTEGER NOT NULL,
    step_id             INTEGER NOT NULL,
    PRIMARY KEY (recipe_equipment_id, position),
    FOREIGN KEY (household_id, recipe_equipment_id) REFERENCES recipe_equipment_new (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, step_id) REFERENCES steps_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO recipe_equipment_steps_new (household_id, recipe_equipment_id, position, step_id)
SELECT e.household_id, es.recipe_equipment_id, es.position, es.step_id
FROM recipe_equipment_steps es JOIN recipe_equipment_new e ON e.id = es.recipe_equipment_id;

CREATE TABLE notes_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    body         TEXT NOT NULL CHECK (body <> ''),
    created_at   TEXT NOT NULL,
    created_by   TEXT NOT NULL CHECK (created_by <> ''),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO notes_new (id, household_id, recipe_id, body, created_at, created_by)
SELECT n.id, r.household_id, n.recipe_id, n.body, n.created_at, n.created_by FROM notes n JOIN recipes_new r ON r.id = n.recipe_id;

CREATE TABLE made_it_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    made_on      TEXT NOT NULL,
    rating       INTEGER,
    comment      TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    created_by   TEXT NOT NULL CHECK (created_by <> ''),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO made_it_new (id, household_id, recipe_id, made_on, rating, comment, created_at, created_by)
SELECT m.id, r.household_id, m.recipe_id, m.made_on, m.rating, m.comment, m.created_at, m.created_by
FROM made_it m JOIN recipes_new r ON r.id = m.recipe_id;

CREATE TABLE revisions_new (
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
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO revisions_new (id, household_id, recipe_id, version, document, change_kind, restored_version, created_at, created_by)
SELECT v.id, r.household_id, v.recipe_id, v.version, v.document, v.change_kind, v.restored_version, v.created_at, v.created_by
FROM revisions v JOIN recipes_new r ON r.id = v.recipe_id;

CREATE TABLE revision_ingredients_new (
    household_id  INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    revision_id   INTEGER NOT NULL,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id),
    PRIMARY KEY (revision_id, ingredient_id),
    FOREIGN KEY (household_id, revision_id) REFERENCES revisions_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO revision_ingredients_new (household_id, revision_id, ingredient_id)
SELECT v.household_id, ri.revision_id, ri.ingredient_id
FROM revision_ingredients ri JOIN revisions_new v ON v.id = ri.revision_id;

CREATE TABLE source_snapshots_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    recipe_id    INTEGER NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('html', 'text', 'ocr', 'llm_response')),
    content      BLOB NOT NULL,
    sha256       TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    created_by   TEXT NOT NULL CHECK (created_by <> ''),
    UNIQUE (recipe_id, kind),
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO source_snapshots_new (id, household_id, recipe_id, kind, content, sha256, created_at, created_by)
SELECT s.id, r.household_id, s.recipe_id, s.kind, s.content, s.sha256, s.created_at, s.created_by
FROM source_snapshots s JOIN recipes_new r ON r.id = s.recipe_id;

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

ALTER TABLE recipes_new RENAME TO recipes;
ALTER TABLE recipe_tags_new RENAME TO recipe_tags;
ALTER TABLE recipe_special_times_new RENAME TO recipe_special_times;
ALTER TABLE components_new RENAME TO components;
ALTER TABLE ingredient_lines_new RENAME TO ingredient_lines;
ALTER TABLE line_qualifiers_new RENAME TO line_qualifiers;
ALTER TABLE steps_new RENAME TO steps;
ALTER TABLE step_dependencies_new RENAME TO step_dependencies;
ALTER TABLE recipe_equipment_new RENAME TO recipe_equipment;
ALTER TABLE recipe_equipment_steps_new RENAME TO recipe_equipment_steps;
ALTER TABLE notes_new RENAME TO notes;
ALTER TABLE made_it_new RENAME TO made_it;
ALTER TABLE revisions_new RENAME TO revisions;
ALTER TABLE revision_ingredients_new RENAME TO revision_ingredients;
ALTER TABLE source_snapshots_new RENAME TO source_snapshots;

CREATE INDEX recipes_cloned_from ON recipes (cloned_from_recipe_id);
CREATE INDEX recipes_household_id ON recipes (household_id);
CREATE INDEX ingredient_lines_ingredient_id ON ingredient_lines (ingredient_id);
CREATE INDEX step_dependencies_depends_on ON step_dependencies (depends_on_step_id);
CREATE INDEX recipe_equipment_equipment_id ON recipe_equipment (equipment_id);
CREATE INDEX notes_recipe_id ON notes (recipe_id);
CREATE INDEX made_it_recipe_id ON made_it (recipe_id);
CREATE INDEX revision_ingredients_ingredient_id ON revision_ingredients (ingredient_id);

INSERT INTO shopping_entries (id, list_id, recipe_id, multiplier)
SELECT id, list_id, recipe_id, multiplier FROM shopping_entries_keep;
DROP TABLE shopping_entries_keep;

-- A clone stays in its origin's household. The origin may be absent (deleted,
-- then the pointer is NULL), which the foreign key already covers.
-- +goose StatementBegin
CREATE TRIGGER recipes_clone_same_household_ai BEFORE INSERT ON recipes
WHEN NEW.cloned_from_recipe_id IS NOT NULL
 AND EXISTS (SELECT 1 FROM recipes WHERE id = NEW.cloned_from_recipe_id AND household_id <> NEW.household_id)
BEGIN
    SELECT RAISE(ABORT, 'cloned_from_recipe_id must name a recipe of the same household');
END;
-- +goose StatementEnd

-- The full-text index (migration 00004) with the household beside the text.
-- household_id is UNINDEXED: it is filtered on, never matched.
CREATE VIRTUAL TABLE recipe_search USING fts5 (
    name, description, ingredients, tags, notes, household_id UNINDEXED,
    tokenize = 'unicode61 remove_diacritics 2'
);

-- Aliases, not a column list: sqlc names view columns from the SELECT.
CREATE VIEW recipe_search_source AS
SELECT r.id AS id, r.name AS name, r.description AS description,
       COALESCE((SELECT group_concat(COALESCE(i.name, '') || ' ' || l.raw, char(10))
                   FROM components c
                   JOIN ingredient_lines l ON l.component_id = c.id
                   LEFT JOIN ingredients i ON i.id = l.ingredient_id
                  WHERE c.recipe_id = r.id), '') AS ingredients,
       COALESCE((SELECT group_concat(tag, char(10)) FROM recipe_tags WHERE recipe_id = r.id), '') AS tags,
       COALESCE((SELECT group_concat(body, char(10)) FROM notes WHERE recipe_id = r.id), '') AS notes,
       r.household_id AS household_id
  FROM recipes r;

-- +goose StatementBegin
CREATE TRIGGER recipe_search_recipes_ai AFTER INSERT ON recipes
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_recipes_au AFTER UPDATE OF name, description ON recipes
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = NEW.id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_tags_ai AFTER INSERT ON recipe_tags
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_tags_ad AFTER DELETE ON recipe_tags
BEGIN
    DELETE FROM recipe_search WHERE rowid = OLD.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = OLD.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_components_ai AFTER INSERT ON components
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_components_ad AFTER DELETE ON components
BEGIN
    DELETE FROM recipe_search WHERE rowid = OLD.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = OLD.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_notes_ai AFTER INSERT ON notes
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_notes_ad AFTER DELETE ON notes
BEGIN
    DELETE FROM recipe_search WHERE rowid = OLD.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = OLD.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_tags_au AFTER UPDATE ON recipe_tags
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_notes_au AFTER UPDATE ON notes
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_components_au AFTER UPDATE ON components
BEGIN
    DELETE FROM recipe_search WHERE rowid = NEW.recipe_id;
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = NEW.recipe_id;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_lines_ai AFTER INSERT ON ingredient_lines
BEGIN
    DELETE FROM recipe_search WHERE rowid = (SELECT recipe_id FROM components WHERE id = NEW.component_id);
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = (SELECT recipe_id FROM components WHERE id = NEW.component_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_lines_au AFTER UPDATE ON ingredient_lines
BEGIN
    DELETE FROM recipe_search WHERE rowid = (SELECT recipe_id FROM components WHERE id = NEW.component_id);
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source WHERE id = (SELECT recipe_id FROM components WHERE id = NEW.component_id);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER recipe_search_lines_ad AFTER DELETE ON ingredient_lines
BEGIN
    DELETE FROM recipe_search WHERE rowid = (SELECT recipe_id FROM components WHERE id = OLD.component_id);
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
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
    INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
    SELECT * FROM recipe_search_source
     WHERE id IN (SELECT c.recipe_id FROM ingredient_lines l JOIN components c ON c.id = l.component_id
                   WHERE l.ingredient_id = NEW.id);
END;
-- +goose StatementEnd

-- Recipes that exist before this migration are indexed here.
INSERT INTO recipe_search (rowid, name, description, ingredients, tags, notes, household_id)
SELECT * FROM recipe_search_source;

-- +goose Down
-- Back to the shape migration 00007 left: household_id is dropped from every
-- table (rebuilt, rows kept) and the index is the one migration 00004 made.
DROP TRIGGER recipes_clone_same_household_ai;
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

CREATE TEMP TABLE shopping_entries_keep AS SELECT id, list_id, recipe_id, multiplier FROM shopping_entries;
DELETE FROM shopping_entries;

CREATE TABLE recipes_old (
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
    version               INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    cloned_from_recipe_id INTEGER REFERENCES recipes_old (id) ON DELETE SET NULL,
    cloned_from_version   INTEGER,
    created_at            TEXT NOT NULL,
    created_by            TEXT NOT NULL CHECK (created_by <> ''),
    updated_at            TEXT NOT NULL,
    updated_by            TEXT NOT NULL CHECK (updated_by <> '')
);
INSERT INTO recipes_old (id, name, description, source_url, author, yield_qty, yield_unit, servings,
    calories_per_serving, rating, prep_ns, cook_ns, total_ns, version, cloned_from_recipe_id, cloned_from_version,
    created_at, created_by, updated_at, updated_by)
SELECT id, name, description, source_url, author, yield_qty, yield_unit, servings,
    calories_per_serving, rating, prep_ns, cook_ns, total_ns, version, cloned_from_recipe_id, cloned_from_version,
    created_at, created_by, updated_at, updated_by
FROM recipes;

CREATE TABLE recipe_tags_old (
    recipe_id INTEGER NOT NULL REFERENCES recipes_old (id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    tag       TEXT NOT NULL,
    PRIMARY KEY (recipe_id, position)
);
INSERT INTO recipe_tags_old (recipe_id, position, tag) SELECT recipe_id, position, tag FROM recipe_tags;

CREATE TABLE recipe_special_times_old (
    recipe_id   INTEGER NOT NULL REFERENCES recipes_old (id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    label       TEXT NOT NULL,
    duration_ns INTEGER NOT NULL CHECK (duration_ns >= 0),
    PRIMARY KEY (recipe_id, position)
);
INSERT INTO recipe_special_times_old (recipe_id, position, label, duration_ns)
SELECT recipe_id, position, label, duration_ns FROM recipe_special_times;

CREATE TABLE components_old (
    id        INTEGER PRIMARY KEY,
    recipe_id INTEGER NOT NULL REFERENCES recipes_old (id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    name      TEXT NOT NULL DEFAULT '',
    UNIQUE (recipe_id, position)
);
INSERT INTO components_old (id, recipe_id, position, name) SELECT id, recipe_id, position, name FROM components;

CREATE TABLE ingredient_lines_old (
    id              INTEGER PRIMARY KEY,
    component_id    INTEGER NOT NULL REFERENCES components_old (id) ON DELETE CASCADE,
    position        INTEGER NOT NULL,
    qty             TEXT,
    unit_id         INTEGER REFERENCES units (id),
    has_package     INTEGER NOT NULL DEFAULT 0 CHECK (has_package IN (0, 1)),
    package_qty     TEXT,
    package_unit_id INTEGER REFERENCES units (id),
    ingredient_id   INTEGER REFERENCES ingredients (id),
    optional        INTEGER NOT NULL DEFAULT 0 CHECK (optional IN (0, 1)),
    rule            TEXT NOT NULL CHECK (rule IN ('linear', 'fixed', 'to_taste')),
    raw             TEXT NOT NULL DEFAULT '',
    UNIQUE (component_id, position)
);
INSERT INTO ingredient_lines_old (id, component_id, position, qty, unit_id, has_package, package_qty,
    package_unit_id, ingredient_id, optional, rule, raw)
SELECT id, component_id, position, qty, unit_id, has_package, package_qty,
    package_unit_id, ingredient_id, optional, rule, raw
FROM ingredient_lines;

CREATE TABLE line_qualifiers_old (
    line_id  INTEGER NOT NULL REFERENCES ingredient_lines_old (id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    kind     TEXT NOT NULL CHECK (kind IN ('preparation', 'state', 'form', 'note')),
    text     TEXT NOT NULL CHECK (text <> ''),
    PRIMARY KEY (line_id, position)
);
INSERT INTO line_qualifiers_old (line_id, position, kind, text) SELECT line_id, position, kind, text FROM line_qualifiers;

CREATE TABLE steps_old (
    id           INTEGER PRIMARY KEY,
    recipe_id    INTEGER NOT NULL REFERENCES recipes_old (id) ON DELETE CASCADE,
    component_id INTEGER NOT NULL REFERENCES components_old (id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    step_key     TEXT NOT NULL CHECK (step_key <> ''),
    text         TEXT NOT NULL DEFAULT '',
    duration_ns  INTEGER NOT NULL DEFAULT 0 CHECK (duration_ns >= 0),
    passive      INTEGER NOT NULL DEFAULT 0 CHECK (passive IN (0, 1)),
    UNIQUE (component_id, position),
    UNIQUE (recipe_id, step_key)
);
INSERT INTO steps_old (id, recipe_id, component_id, position, step_key, text, duration_ns, passive)
SELECT id, recipe_id, component_id, position, step_key, text, duration_ns, passive FROM steps;

CREATE TABLE step_dependencies_old (
    step_id            INTEGER NOT NULL REFERENCES steps_old (id) ON DELETE CASCADE,
    position           INTEGER NOT NULL,
    depends_on_step_id INTEGER NOT NULL REFERENCES steps_old (id) ON DELETE CASCADE,
    PRIMARY KEY (step_id, position),
    CHECK (step_id <> depends_on_step_id)
);
INSERT INTO step_dependencies_old (step_id, position, depends_on_step_id)
SELECT step_id, position, depends_on_step_id FROM step_dependencies;

CREATE TABLE recipe_equipment_old (
    id           INTEGER PRIMARY KEY,
    recipe_id    INTEGER NOT NULL REFERENCES recipes_old (id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    equipment_id INTEGER NOT NULL REFERENCES equipment (id),
    qty          TEXT,
    UNIQUE (recipe_id, position)
);
INSERT INTO recipe_equipment_old (id, recipe_id, position, equipment_id, qty)
SELECT id, recipe_id, position, equipment_id, qty FROM recipe_equipment;

CREATE TABLE recipe_equipment_steps_old (
    recipe_equipment_id INTEGER NOT NULL REFERENCES recipe_equipment_old (id) ON DELETE CASCADE,
    position            INTEGER NOT NULL,
    step_id             INTEGER NOT NULL REFERENCES steps_old (id) ON DELETE CASCADE,
    PRIMARY KEY (recipe_equipment_id, position)
);
INSERT INTO recipe_equipment_steps_old (recipe_equipment_id, position, step_id)
SELECT recipe_equipment_id, position, step_id FROM recipe_equipment_steps;

CREATE TABLE notes_old (
    id         INTEGER PRIMARY KEY,
    recipe_id  INTEGER NOT NULL REFERENCES recipes_old (id) ON DELETE CASCADE,
    body       TEXT NOT NULL CHECK (body <> ''),
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (created_by <> '')
);
INSERT INTO notes_old (id, recipe_id, body, created_at, created_by)
SELECT id, recipe_id, body, created_at, created_by FROM notes;

CREATE TABLE made_it_old (
    id         INTEGER PRIMARY KEY,
    recipe_id  INTEGER NOT NULL REFERENCES recipes_old (id) ON DELETE CASCADE,
    made_on    TEXT NOT NULL,
    rating     INTEGER,
    comment    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (created_by <> '')
);
INSERT INTO made_it_old (id, recipe_id, made_on, rating, comment, created_at, created_by)
SELECT id, recipe_id, made_on, rating, comment, created_at, created_by FROM made_it;

CREATE TABLE revisions_old (
    id               INTEGER PRIMARY KEY,
    recipe_id        INTEGER NOT NULL REFERENCES recipes_old (id) ON DELETE CASCADE,
    version          INTEGER NOT NULL CHECK (version >= 1),
    document         TEXT NOT NULL,
    change_kind      TEXT NOT NULL CHECK (change_kind IN ('edit', 'restore')),
    restored_version INTEGER,
    created_at       TEXT NOT NULL,
    created_by       TEXT NOT NULL CHECK (created_by <> ''),
    UNIQUE (recipe_id, version)
);
INSERT INTO revisions_old (id, recipe_id, version, document, change_kind, restored_version, created_at, created_by)
SELECT id, recipe_id, version, document, change_kind, restored_version, created_at, created_by FROM revisions;

CREATE TABLE revision_ingredients_old (
    revision_id   INTEGER NOT NULL REFERENCES revisions_old (id) ON DELETE CASCADE,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id),
    PRIMARY KEY (revision_id, ingredient_id)
);
INSERT INTO revision_ingredients_old (revision_id, ingredient_id)
SELECT revision_id, ingredient_id FROM revision_ingredients;

CREATE TABLE source_snapshots_old (
    id         INTEGER PRIMARY KEY,
    recipe_id  INTEGER NOT NULL REFERENCES recipes_old (id) ON DELETE CASCADE,
    kind       TEXT NOT NULL CHECK (kind IN ('html', 'text', 'ocr', 'llm_response')),
    content    BLOB NOT NULL,
    sha256     TEXT NOT NULL,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL CHECK (created_by <> ''),
    UNIQUE (recipe_id, kind)
);
INSERT INTO source_snapshots_old (id, recipe_id, kind, content, sha256, created_at, created_by)
SELECT id, recipe_id, kind, content, sha256, created_at, created_by FROM source_snapshots;

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

ALTER TABLE recipes_old RENAME TO recipes;
ALTER TABLE recipe_tags_old RENAME TO recipe_tags;
ALTER TABLE recipe_special_times_old RENAME TO recipe_special_times;
ALTER TABLE components_old RENAME TO components;
ALTER TABLE ingredient_lines_old RENAME TO ingredient_lines;
ALTER TABLE line_qualifiers_old RENAME TO line_qualifiers;
ALTER TABLE steps_old RENAME TO steps;
ALTER TABLE step_dependencies_old RENAME TO step_dependencies;
ALTER TABLE recipe_equipment_old RENAME TO recipe_equipment;
ALTER TABLE recipe_equipment_steps_old RENAME TO recipe_equipment_steps;
ALTER TABLE notes_old RENAME TO notes;
ALTER TABLE made_it_old RENAME TO made_it;
ALTER TABLE revisions_old RENAME TO revisions;
ALTER TABLE revision_ingredients_old RENAME TO revision_ingredients;
ALTER TABLE source_snapshots_old RENAME TO source_snapshots;

CREATE INDEX recipes_cloned_from ON recipes (cloned_from_recipe_id);
CREATE INDEX ingredient_lines_ingredient_id ON ingredient_lines (ingredient_id);
CREATE INDEX step_dependencies_depends_on ON step_dependencies (depends_on_step_id);
CREATE INDEX recipe_equipment_equipment_id ON recipe_equipment (equipment_id);
CREATE INDEX notes_recipe_id ON notes (recipe_id);
CREATE INDEX made_it_recipe_id ON made_it (recipe_id);
CREATE INDEX revision_ingredients_ingredient_id ON revision_ingredients (ingredient_id);

INSERT INTO shopping_entries (id, list_id, recipe_id, multiplier)
SELECT id, list_id, recipe_id, multiplier FROM shopping_entries_keep;
DROP TABLE shopping_entries_keep;

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

