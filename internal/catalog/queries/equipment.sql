-- name: CreateEquipment :one
INSERT INTO equipment (name, reviewed, sized) VALUES (?, ?, ?)
RETURNING id;

-- name: UpdateEquipment :execrows
UPDATE equipment SET name = ?, reviewed = ?, sized = ? WHERE id = ?;

-- name: GetEquipment :one
SELECT id, name, reviewed, sized FROM equipment WHERE id = ?;

-- name: ListEquipment :many
SELECT id, name, reviewed, sized FROM equipment ORDER BY name;

-- name: ListUnreviewedEquipment :many
SELECT id, name, reviewed, sized FROM equipment WHERE reviewed = 0 ORDER BY name;

-- name: LookupEquipmentID :one
SELECT id FROM equipment WHERE name = sqlc.arg(term)
UNION ALL
SELECT equipment_id FROM equipment_aliases WHERE alias = sqlc.arg(term)
ORDER BY 1 LIMIT 1;

-- name: ListEquipmentAliases :many
SELECT alias FROM equipment_aliases WHERE equipment_id = ? ORDER BY alias;

-- name: ListAllEquipmentAliases :many
SELECT equipment_id, alias FROM equipment_aliases ORDER BY alias;

-- name: DeleteEquipmentAliases :exec
DELETE FROM equipment_aliases WHERE equipment_id = ?;

-- name: AddEquipmentAlias :exec
INSERT INTO equipment_aliases (alias, equipment_id) VALUES (?, ?);

-- name: DeleteEquipment :execrows
DELETE FROM equipment WHERE id = ?;
