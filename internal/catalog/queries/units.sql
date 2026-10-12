-- name: CreateUnit :one
INSERT INTO units (name, system, dimension, factor, reviewed)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateUnit :execrows
UPDATE units
SET name = ?, system = ?, dimension = ?, factor = ?, reviewed = ?
WHERE id = ?;

-- name: GetUnit :one
SELECT id, name, system, dimension, factor, reviewed FROM units WHERE id = ?;

-- name: ListUnits :many
SELECT id, name, system, dimension, factor, reviewed FROM units ORDER BY name;

-- name: ListUnreviewedUnits :many
SELECT id, name, system, dimension, factor, reviewed
FROM units WHERE reviewed = 0 ORDER BY name;

-- name: LookupUnitID :one
SELECT id FROM units WHERE name = sqlc.arg(term)
UNION ALL
SELECT unit_id FROM unit_aliases WHERE alias = sqlc.arg(term)
ORDER BY 1 LIMIT 1;

-- name: ListUnitAliases :many
SELECT alias FROM unit_aliases WHERE unit_id = ? ORDER BY alias;

-- name: ListAllUnitAliases :many
SELECT unit_id, alias FROM unit_aliases ORDER BY alias;

-- name: DeleteUnitAliases :exec
DELETE FROM unit_aliases WHERE unit_id = ?;

-- name: AddUnitAlias :exec
INSERT INTO unit_aliases (alias, unit_id) VALUES (?, ?);

-- name: DeleteUnit :execrows
DELETE FROM units WHERE id = ?;
