-- +goose Up
-- Two names recipe pages use for catalog equipment (ADR-00024): "mandoline
-- slicer" for the mandoline and "pastry blender" for the pastry cutter. The
-- catalog lacked them because the ingest tests used those strings as known
-- unmatched equipment; the tests now use fictional names. Written by hand, not
-- rendered from equipment_additions.json, which migration 00015 is pinned to by
-- TestMigrationHoldsRenderedEquipmentAdditions. Names and aliases share one
-- namespace, so an alias is added only when no equipment row already uses the
-- term as its name or as an alias (the old save flow may have created one).
INSERT INTO equipment_aliases (alias, equipment_id)
SELECT 'mandoline slicer', (SELECT id FROM equipment WHERE name = 'mandoline')
WHERE NOT EXISTS (SELECT 1 FROM equipment WHERE name = 'mandoline slicer')
  AND NOT EXISTS (SELECT 1 FROM equipment_aliases WHERE alias = 'mandoline slicer');
INSERT INTO equipment_aliases (alias, equipment_id)
SELECT 'pastry blender', (SELECT id FROM equipment WHERE name = 'pastry cutter')
WHERE NOT EXISTS (SELECT 1 FROM equipment WHERE name = 'pastry blender')
  AND NOT EXISTS (SELECT 1 FROM equipment_aliases WHERE alias = 'pastry blender');

-- +goose Down
-- Nothing is removed. Up skips a term an operator already uses, and Down cannot
-- tell an alias it added from one the operator created under the same term; an
-- extra alias is harmless.
SELECT 1;
