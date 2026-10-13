-- +goose Up
-- The ingredient catalog gains a per-household overlay (ADR-00018, ADR-00008).
-- ingredients.household_id is NULL for a central entry, which every household
-- reads and only the operator edits, and a household's id for an overlay entry
-- that household's ingestion or editing created. Names are unique within a
-- layer, not across the table, so two households may each hold their own
-- "ghee". An entry keeps its id when the operator promotes it (household_id
-- becomes NULL), so the promoting household's rows need no rewrite.
--
-- ingredient_aliases gains the same column: NULL for an alias of a central
-- entry, a household's id for an alias of one of its overlay entries or its own
-- alias for a central entry. ingredient_aisle_overrides is a household's own
-- aisle for a central entry. Deleting a household cascades through all three.
-- The catalog store keeps an alias in its scope (a central alias on a central
-- entry, a household alias on a central entry or one of its own).
-- suggested_at marks an overlay entry its household flagged for promotion.
--
-- SQLite cannot drop the old global UNIQUE (name) constraints, so ingredients
-- and ingredient_aliases are rebuilt. Rows keep their ids. Tables that
-- reference ingredients(id) with a plain foreign key keep their rows, so the
-- rebuild runs with foreign keys deferred: the drop counts those rows as
-- violations and re-inserting the same ids clears them before the commit.
-- Dropping ingredients also drops the trigger on it, which is recreated below.
--
-- Existing rows: a reviewed ingredient (the whole seed is reviewed) becomes
-- central. An unreviewed one is an ingestion proposal nobody has checked, so it
-- lands in the overlay (ADR-00018), of the household with the lowest id, as
-- migrations 00008 and 00009 assigned the existing recipes and lists, together
-- with its aliases. With no household it stays central, because the overlay
-- needs an owner and nothing is invented.

PRAGMA defer_foreign_keys = ON;

CREATE TABLE ingredients_keep (
    id               INTEGER NOT NULL,
    name             TEXT NOT NULL,
    aisle            TEXT NOT NULL,
    density_g_per_ml REAL,
    usda_fdc_id      INTEGER,
    reviewed         INTEGER NOT NULL
);
INSERT INTO ingredients_keep SELECT id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed FROM ingredients;

CREATE TABLE ingredient_aliases_keep (
    alias         TEXT NOT NULL,
    ingredient_id INTEGER NOT NULL
);
INSERT INTO ingredient_aliases_keep SELECT alias, ingredient_id FROM ingredient_aliases;

DROP TABLE ingredient_aliases;
DROP TABLE ingredients;

CREATE TABLE ingredients (
    id               INTEGER PRIMARY KEY,
    household_id     INTEGER REFERENCES households (id) ON DELETE CASCADE,
    name             TEXT NOT NULL CHECK (name <> ''),
    aisle            TEXT NOT NULL CHECK (aisle <> ''),
    density_g_per_ml REAL CHECK (density_g_per_ml IS NULL OR density_g_per_ml > 0),
    usda_fdc_id      INTEGER CHECK (usda_fdc_id IS NULL OR usda_fdc_id > 0),
    reviewed         INTEGER NOT NULL DEFAULT 0 CHECK (reviewed IN (0, 1)),
    suggested_at     TEXT,
    CHECK (suggested_at IS NULL OR household_id IS NOT NULL)
);
INSERT INTO ingredients (id, household_id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed)
SELECT id, CASE WHEN reviewed = 0 THEN (SELECT min(id) FROM households) END,
       name, aisle, density_g_per_ml, usda_fdc_id, reviewed
FROM ingredients_keep;

CREATE UNIQUE INDEX ingredients_central_name ON ingredients (name) WHERE household_id IS NULL;
CREATE UNIQUE INDEX ingredients_household_name ON ingredients (household_id, name) WHERE household_id IS NOT NULL;
CREATE UNIQUE INDEX ingredients_central_usda ON ingredients (usda_fdc_id)
    WHERE household_id IS NULL AND usda_fdc_id IS NOT NULL;
CREATE UNIQUE INDEX ingredients_household_usda ON ingredients (household_id, usda_fdc_id)
    WHERE household_id IS NOT NULL AND usda_fdc_id IS NOT NULL;
CREATE INDEX ingredients_suggested ON ingredients (suggested_at) WHERE suggested_at IS NOT NULL;

CREATE TABLE ingredient_aliases (
    id            INTEGER PRIMARY KEY,
    household_id  INTEGER REFERENCES households (id) ON DELETE CASCADE,
    alias         TEXT NOT NULL CHECK (alias <> ''),
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id) ON DELETE CASCADE
);
INSERT INTO ingredient_aliases (household_id, alias, ingredient_id)
SELECT i.household_id, k.alias, k.ingredient_id
FROM ingredient_aliases_keep k JOIN ingredients i ON i.id = k.ingredient_id;

CREATE UNIQUE INDEX ingredient_aliases_central ON ingredient_aliases (alias) WHERE household_id IS NULL;
CREATE UNIQUE INDEX ingredient_aliases_household ON ingredient_aliases (household_id, alias) WHERE household_id IS NOT NULL;
CREATE INDEX ingredient_aliases_ingredient_id ON ingredient_aliases (ingredient_id);

DROP TABLE ingredient_aliases_keep;
DROP TABLE ingredients_keep;

CREATE TABLE ingredient_aisle_overrides (
    household_id  INTEGER NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id) ON DELETE CASCADE,
    aisle         TEXT NOT NULL CHECK (aisle <> ''),
    PRIMARY KEY (household_id, ingredient_id)
);
CREATE INDEX ingredient_aisle_overrides_ingredient_id ON ingredient_aisle_overrides (ingredient_id);

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

-- +goose Down
-- Back to one global catalog. Overlay entries become ordinary entries, so a
-- name or alias held by two households fails the copy on the old UNIQUE
-- constraint and the migration aborts rather than merging them silently.
-- Household aisle overrides and suggestions are dropped.
PRAGMA defer_foreign_keys = ON;

CREATE TABLE ingredients_keep (
    id               INTEGER NOT NULL,
    name             TEXT NOT NULL,
    aisle            TEXT NOT NULL,
    density_g_per_ml REAL,
    usda_fdc_id      INTEGER,
    reviewed         INTEGER NOT NULL
);
INSERT INTO ingredients_keep SELECT id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed FROM ingredients;

CREATE TABLE ingredient_aliases_keep (
    alias         TEXT NOT NULL,
    ingredient_id INTEGER NOT NULL
);
INSERT INTO ingredient_aliases_keep
SELECT a.alias, a.ingredient_id FROM ingredient_aliases a JOIN ingredients i ON i.id = a.ingredient_id
 WHERE a.household_id IS NULL OR a.household_id = i.household_id;

DROP TABLE ingredient_aisle_overrides;
DROP TABLE ingredient_aliases;
DROP TABLE ingredients;

CREATE TABLE ingredients (
    id               INTEGER PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE CHECK (name <> ''),
    aisle            TEXT NOT NULL CHECK (aisle <> ''),
    density_g_per_ml REAL CHECK (density_g_per_ml IS NULL OR density_g_per_ml > 0),
    usda_fdc_id      INTEGER UNIQUE CHECK (usda_fdc_id IS NULL OR usda_fdc_id > 0),
    reviewed         INTEGER NOT NULL DEFAULT 0 CHECK (reviewed IN (0, 1))
);
INSERT INTO ingredients SELECT id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed FROM ingredients_keep;

CREATE TABLE ingredient_aliases (
    alias         TEXT PRIMARY KEY CHECK (alias <> ''),
    ingredient_id INTEGER NOT NULL REFERENCES ingredients (id) ON DELETE CASCADE
);
INSERT INTO ingredient_aliases SELECT alias, ingredient_id FROM ingredient_aliases_keep;
CREATE INDEX ingredient_aliases_ingredient_id ON ingredient_aliases (ingredient_id);

DROP TABLE ingredient_aliases_keep;
DROP TABLE ingredients_keep;

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
