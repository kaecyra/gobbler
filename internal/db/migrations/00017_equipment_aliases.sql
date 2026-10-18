-- +goose Up
-- Two names recipe pages use for catalog equipment (ADR-00024): "mandoline
-- slicer" for the mandoline and "pastry blender" for the pastry cutter. The
-- catalog lacked them because the ingest tests used those strings as known
-- unmatched equipment; the tests now use fictional names. Written by hand, not
-- rendered from equipment_additions.json, which migration 00015 is pinned to by
-- TestMigrationHoldsRenderedEquipmentAdditions. An existing alias is left alone.
INSERT INTO equipment_aliases (alias, equipment_id) VALUES ('mandoline slicer', (SELECT id FROM equipment WHERE name = 'mandoline')) ON CONFLICT (alias) DO NOTHING;
INSERT INTO equipment_aliases (alias, equipment_id) VALUES ('pastry blender', (SELECT id FROM equipment WHERE name = 'pastry cutter')) ON CONFLICT (alias) DO NOTHING;

-- +goose Down
DELETE FROM equipment_aliases WHERE alias IN ('mandoline slicer', 'pastry blender');
