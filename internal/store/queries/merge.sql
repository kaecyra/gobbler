-- These re-point the store's tables that reference ingredients(id) during a
-- catalog ingredient merge (RegisterMergeHooks).

-- name: RepointIngredientLines :exec
UPDATE ingredient_lines SET ingredient_id = sqlc.arg(winner_id) WHERE ingredient_id = sqlc.arg(loser_id);

-- name: ListRevisionsNamingIngredient :many
SELECT r.id, r.document FROM revisions r
JOIN revision_ingredients ri ON ri.revision_id = r.id
WHERE ri.ingredient_id = ?;

-- name: UpdateRevisionDocument :exec
UPDATE revisions SET document = ? WHERE id = ?;

-- name: RepointRevisionIndex :exec
UPDATE OR IGNORE revision_ingredients SET ingredient_id = sqlc.arg(winner_id) WHERE ingredient_id = sqlc.arg(loser_id);

-- name: DeleteRevisionIndex :exec
DELETE FROM revision_ingredients WHERE ingredient_id = ?;
