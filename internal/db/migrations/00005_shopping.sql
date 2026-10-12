-- +goose Up
-- Shopping lists (ADR-00009): a list holds recipe entries (recipe plus
-- multiplier) and manual items; the pantry is the set of ingredients always on
-- hand; check state is one row per checked item. The shopping package owns the
-- SQL (internal/shopping).
--
-- Multipliers and quantities are exact reduced fractions as text ("n/d", and
-- "n/d..n/d" for a range), the same form recipe lines use. Timestamps are
-- RFC 3339 UTC text.
--
-- recipe_id and the ingredient references are plain foreign keys (no cascade):
-- deleting a recipe that a list uses fails instead of silently emptying the
-- list. The tables that reference ingredients(id) (shopping_manual_items,
-- shopping_pantry) and the ingredient-keyed check rows need a
-- catalog.OnIngredientMerge hook: the shopping store provides it as
-- RegisterMergeHooks, which the UI node (#25) must wire at start-up. Until it
-- is wired, these restricted foreign keys refuse a merge of an ingredient that
-- shopping rows reference.

CREATE TABLE shopping_lists (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL CHECK (name <> ''),
    created_at TEXT NOT NULL
);

CREATE TABLE shopping_entries (
    id         INTEGER PRIMARY KEY,
    list_id    INTEGER NOT NULL REFERENCES shopping_lists (id) ON DELETE CASCADE,
    recipe_id  INTEGER NOT NULL REFERENCES recipes (id),
    multiplier TEXT NOT NULL CHECK (multiplier <> ''),
    UNIQUE (list_id, recipe_id)
);
CREATE INDEX shopping_entries_recipe_id ON shopping_entries (recipe_id);

-- A manual item is free text, a canonical ingredient, or both. Its amount is
-- laid out like an ingredient line's.
CREATE TABLE shopping_manual_items (
    id              INTEGER PRIMARY KEY,
    list_id         INTEGER NOT NULL REFERENCES shopping_lists (id) ON DELETE CASCADE,
    text            TEXT NOT NULL DEFAULT '',
    ingredient_id   INTEGER REFERENCES ingredients (id),
    qty             TEXT,
    unit_id         INTEGER REFERENCES units (id),
    has_package     INTEGER NOT NULL DEFAULT 0 CHECK (has_package IN (0, 1)),
    package_qty     TEXT,
    package_unit_id INTEGER REFERENCES units (id),
    CHECK (text <> '' OR ingredient_id IS NOT NULL)
);
CREATE INDEX shopping_manual_items_list_id ON shopping_manual_items (list_id);
CREATE INDEX shopping_manual_items_ingredient_id ON shopping_manual_items (ingredient_id);

CREATE TABLE shopping_pantry (
    ingredient_id INTEGER PRIMARY KEY REFERENCES ingredients (id)
);

-- One row per checked item. item_key is the stable identity of a generated
-- item (shopping.ItemKey), so the row survives regeneration while the item
-- exists. It has no foreign key: the item is derived, not stored.
CREATE TABLE shopping_checks (
    list_id    INTEGER NOT NULL REFERENCES shopping_lists (id) ON DELETE CASCADE,
    item_key   TEXT NOT NULL CHECK (item_key <> ''),
    checked_at TEXT NOT NULL,
    PRIMARY KEY (list_id, item_key)
);

-- +goose Down
DROP TABLE shopping_checks;
DROP TABLE shopping_pantry;
DROP TABLE shopping_manual_items;
DROP TABLE shopping_entries;
DROP TABLE shopping_lists;
