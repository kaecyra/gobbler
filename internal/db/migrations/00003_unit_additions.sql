-- +goose Up
-- Unit aliases and units the parser used to own (issue 65): the catalog is the
-- one owner of unit spellings. Rendered by genseed -unit-additions from
-- internal/catalog/seed/unit_additions.json. "piece" and "pieces" stay aliases
-- of "each".

INSERT INTO unit_aliases (alias, unit_id) VALUES ('gr', (SELECT id FROM units WHERE name = 'g'));
INSERT INTO unit_aliases (alias, unit_id) VALUES ('c', (SELECT id FROM units WHERE name = 'cup'));
INSERT INTO units (name, system, dimension, factor, reviewed) VALUES ('bag', 'none', 'other', NULL, 1);
INSERT INTO unit_aliases (alias, unit_id) VALUES ('bags', (SELECT id FROM units WHERE name = 'bag'));
INSERT INTO units (name, system, dimension, factor, reviewed) VALUES ('box', 'none', 'other', NULL, 1);
INSERT INTO unit_aliases (alias, unit_id) VALUES ('boxes', (SELECT id FROM units WHERE name = 'box'));

-- +goose Down
DELETE FROM unit_aliases WHERE alias IN ('gr', 'c', 'bags', 'boxes');
DELETE FROM units WHERE name IN ('bag', 'box');
