-- +goose Up
-- Unit aliases and units the parser used to own (issue 65): the catalog is the
-- one owner of unit spellings. Rendered by genseed -unit-additions from
-- internal/catalog/seed/unit_additions.json. "piece" and "pieces" stay aliases
-- of "each". The statements tolerate rows the old save flow already created: an
-- existing unit of the same name is adopted (marked reviewed), an existing alias
-- is left alone.

INSERT INTO unit_aliases (alias, unit_id) VALUES ('gr', (SELECT id FROM units WHERE name = 'g')) ON CONFLICT (alias) DO NOTHING;
INSERT INTO unit_aliases (alias, unit_id) VALUES ('c', (SELECT id FROM units WHERE name = 'cup')) ON CONFLICT (alias) DO NOTHING;
INSERT INTO units (name, system, dimension, factor, reviewed) VALUES ('bag', 'none', 'other', NULL, 1) ON CONFLICT (name) DO UPDATE SET reviewed = 1;
INSERT INTO unit_aliases (alias, unit_id) VALUES ('bags', (SELECT id FROM units WHERE name = 'bag')) ON CONFLICT (alias) DO NOTHING;
INSERT INTO units (name, system, dimension, factor, reviewed) VALUES ('box', 'none', 'other', NULL, 1) ON CONFLICT (name) DO UPDATE SET reviewed = 1;
INSERT INTO unit_aliases (alias, unit_id) VALUES ('boxes', (SELECT id FROM units WHERE name = 'box')) ON CONFLICT (alias) DO NOTHING;

-- +goose Down
DELETE FROM unit_aliases WHERE alias IN ('gr', 'c', 'bags', 'boxes');
-- The bag and box units stay: Up may have adopted rows that already existed
-- (and recipe lines may reference them), so Down cannot tell them from rows it
-- created, and an extra reviewed unit is harmless.
