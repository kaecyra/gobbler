-- +goose Up
-- Shopping data belongs to a household (ADR-00018): household_id (NOT NULL, a
-- foreign key to households, ON DELETE CASCADE) on lists, entries, manual
-- items, checks and the pantry. The pantry was one global set; it is now one
-- set per household, so its key is (household_id, ingredient_id).
--
-- Every table is tied to its list's household by a composite foreign key
-- (household_id, list_id) onto shopping_lists UNIQUE (household_id, id), and an
-- entry to its recipe's household by (household_id, recipe_id) onto recipes
-- UNIQUE (household_id, id) (migration 00008), so a row can never name another
-- household's list or recipe. Deleting a household cascades to its lists (and
-- through them to every row below) and to its recipes. The entry-to-recipe key
-- stays NO ACTION, so deleting a single recipe that a list uses still fails
-- instead of silently emptying the list (ADR-00009); SQLite checks it at the
-- end of the statement, after the cascade has removed the entries, so deleting
-- the household itself cannot trip it.
--
-- SQLite cannot add a NOT NULL column that references another table, so each
-- table is rebuilt (create the new shape, copy, drop the old, rename). Rows
-- keep their ids. No deployment holds data (ADR-00018), but a database that
-- already has shopping rows must still migrate: they are assigned to the
-- household with the lowest id, and if there are rows and no household at all,
-- the copy fails on NOT NULL household_id and the migration aborts rather than
-- inventing an owner.

CREATE TABLE shopping_lists_new (
    id               INTEGER PRIMARY KEY,
    household_id     INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    name             TEXT NOT NULL CHECK (name <> ''),
    created_at       TEXT NOT NULL,
    include_optional INTEGER NOT NULL DEFAULT 0 CHECK (include_optional IN (0, 1)),
    UNIQUE (household_id, id)
);
INSERT INTO shopping_lists_new (id, household_id, name, created_at, include_optional)
SELECT id, (SELECT min(id) FROM households), name, created_at, include_optional FROM shopping_lists;

CREATE TABLE shopping_entries_new (
    id           INTEGER PRIMARY KEY,
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    list_id      INTEGER NOT NULL,
    recipe_id    INTEGER NOT NULL,
    multiplier   TEXT NOT NULL CHECK (multiplier <> ''),
    UNIQUE (list_id, recipe_id),
    FOREIGN KEY (household_id, list_id) REFERENCES shopping_lists_new (household_id, id) ON DELETE CASCADE,
    FOREIGN KEY (household_id, recipe_id) REFERENCES recipes (household_id, id)
);
INSERT INTO shopping_entries_new (id, household_id, list_id, recipe_id, multiplier)
SELECT e.id, l.household_id, e.list_id, e.recipe_id, e.multiplier
FROM shopping_entries e JOIN shopping_lists_new l ON l.id = e.list_id;

CREATE TABLE shopping_manual_items_new (
    id              INTEGER PRIMARY KEY,
    household_id    INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    list_id         INTEGER NOT NULL,
    text            TEXT NOT NULL DEFAULT '',
    ingredient_id   INTEGER REFERENCES ingredients (id),
    qty             TEXT,
    unit_id         INTEGER REFERENCES units (id),
    has_package     INTEGER NOT NULL DEFAULT 0 CHECK (has_package IN (0, 1)),
    package_qty     TEXT,
    package_unit_id INTEGER REFERENCES units (id),
    CHECK (text <> '' OR ingredient_id IS NOT NULL),
    FOREIGN KEY (household_id, list_id) REFERENCES shopping_lists_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO shopping_manual_items_new (id, household_id, list_id, text, ingredient_id, qty, unit_id, has_package,
    package_qty, package_unit_id)
SELECT m.id, l.household_id, m.list_id, m.text, m.ingredient_id, m.qty, m.unit_id, m.has_package,
    m.package_qty, m.package_unit_id
FROM shopping_manual_items m JOIN shopping_lists_new l ON l.id = m.list_id;

CREATE TABLE shopping_pantry_new (
    household_id  INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id),
    PRIMARY KEY (household_id, ingredient_id)
);
INSERT INTO shopping_pantry_new (household_id, ingredient_id)
SELECT (SELECT min(id) FROM households), ingredient_id FROM shopping_pantry;

CREATE TABLE shopping_checks_new (
    household_id INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    list_id      INTEGER NOT NULL,
    item_key     TEXT NOT NULL CHECK (item_key <> ''),
    checked_at   TEXT NOT NULL,
    PRIMARY KEY (list_id, item_key),
    FOREIGN KEY (household_id, list_id) REFERENCES shopping_lists_new (household_id, id) ON DELETE CASCADE
);
INSERT INTO shopping_checks_new (household_id, list_id, item_key, checked_at)
SELECT l.household_id, c.list_id, c.item_key, c.checked_at
FROM shopping_checks c JOIN shopping_lists_new l ON l.id = c.list_id;

DROP TABLE shopping_checks;
DROP TABLE shopping_pantry;
DROP TABLE shopping_manual_items;
DROP TABLE shopping_entries;
DROP TABLE shopping_lists;

ALTER TABLE shopping_lists_new RENAME TO shopping_lists;
ALTER TABLE shopping_entries_new RENAME TO shopping_entries;
ALTER TABLE shopping_manual_items_new RENAME TO shopping_manual_items;
ALTER TABLE shopping_pantry_new RENAME TO shopping_pantry;
ALTER TABLE shopping_checks_new RENAME TO shopping_checks;

CREATE INDEX shopping_entries_recipe_id ON shopping_entries (household_id, recipe_id);
CREATE INDEX shopping_manual_items_list_id ON shopping_manual_items (list_id);
CREATE INDEX shopping_manual_items_ingredient_id ON shopping_manual_items (ingredient_id);
CREATE INDEX shopping_pantry_ingredient_id ON shopping_pantry (ingredient_id);

-- +goose Down
-- Back to the shape migration 00006 left: household_id is dropped from every
-- table (rebuilt, rows kept). The pantry is global again, so the households'
-- pantries are merged into one set.
CREATE TABLE shopping_lists_old (
    id               INTEGER PRIMARY KEY,
    name             TEXT NOT NULL CHECK (name <> ''),
    created_at       TEXT NOT NULL,
    include_optional INTEGER NOT NULL DEFAULT 0 CHECK (include_optional IN (0, 1))
);
INSERT INTO shopping_lists_old (id, name, created_at, include_optional)
SELECT id, name, created_at, include_optional FROM shopping_lists;

CREATE TABLE shopping_entries_old (
    id         INTEGER PRIMARY KEY,
    list_id    INTEGER NOT NULL REFERENCES shopping_lists_old (id) ON DELETE CASCADE,
    recipe_id  INTEGER NOT NULL REFERENCES recipes (id),
    multiplier TEXT NOT NULL CHECK (multiplier <> ''),
    UNIQUE (list_id, recipe_id)
);
INSERT INTO shopping_entries_old (id, list_id, recipe_id, multiplier)
SELECT id, list_id, recipe_id, multiplier FROM shopping_entries;

CREATE TABLE shopping_manual_items_old (
    id              INTEGER PRIMARY KEY,
    list_id         INTEGER NOT NULL REFERENCES shopping_lists_old (id) ON DELETE CASCADE,
    text            TEXT NOT NULL DEFAULT '',
    ingredient_id   INTEGER REFERENCES ingredients (id),
    qty             TEXT,
    unit_id         INTEGER REFERENCES units (id),
    has_package     INTEGER NOT NULL DEFAULT 0 CHECK (has_package IN (0, 1)),
    package_qty     TEXT,
    package_unit_id INTEGER REFERENCES units (id),
    CHECK (text <> '' OR ingredient_id IS NOT NULL)
);
INSERT INTO shopping_manual_items_old (id, list_id, text, ingredient_id, qty, unit_id, has_package, package_qty,
    package_unit_id)
SELECT id, list_id, text, ingredient_id, qty, unit_id, has_package, package_qty, package_unit_id
FROM shopping_manual_items;

CREATE TABLE shopping_pantry_old (
    ingredient_id INTEGER PRIMARY KEY REFERENCES ingredients (id)
);
INSERT OR IGNORE INTO shopping_pantry_old (ingredient_id) SELECT ingredient_id FROM shopping_pantry;

CREATE TABLE shopping_checks_old (
    list_id    INTEGER NOT NULL REFERENCES shopping_lists_old (id) ON DELETE CASCADE,
    item_key   TEXT NOT NULL CHECK (item_key <> ''),
    checked_at TEXT NOT NULL,
    PRIMARY KEY (list_id, item_key)
);
INSERT INTO shopping_checks_old (list_id, item_key, checked_at)
SELECT list_id, item_key, checked_at FROM shopping_checks;

DROP TABLE shopping_checks;
DROP TABLE shopping_pantry;
DROP TABLE shopping_manual_items;
DROP TABLE shopping_entries;
DROP TABLE shopping_lists;

ALTER TABLE shopping_lists_old RENAME TO shopping_lists;
ALTER TABLE shopping_entries_old RENAME TO shopping_entries;
ALTER TABLE shopping_manual_items_old RENAME TO shopping_manual_items;
ALTER TABLE shopping_pantry_old RENAME TO shopping_pantry;
ALTER TABLE shopping_checks_old RENAME TO shopping_checks;

CREATE INDEX shopping_entries_recipe_id ON shopping_entries (recipe_id);
CREATE INDEX shopping_manual_items_list_id ON shopping_manual_items (list_id);
CREATE INDEX shopping_manual_items_ingredient_id ON shopping_manual_items (ingredient_id);
