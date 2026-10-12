-- +goose Up
-- Whether a shopping list keeps the optional lines of its recipes (ADR-00009).
-- It is a property of the list, so every view of the list agrees. The default,
-- 0, drops optional lines: what every list did before this column existed.
ALTER TABLE shopping_lists
    ADD COLUMN include_optional INTEGER NOT NULL DEFAULT 0 CHECK (include_optional IN (0, 1));

-- +goose Down
ALTER TABLE shopping_lists DROP COLUMN include_optional;
