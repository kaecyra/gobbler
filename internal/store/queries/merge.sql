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

-- name: ListProposalsNamingIngredient :many
SELECT p.id, p.kind, p.document FROM proposals p
JOIN proposal_ingredients pi ON pi.proposal_id = p.id
WHERE pi.ingredient_id = ?;

-- name: UpdateProposalDocument :exec
UPDATE proposals SET document = ? WHERE id = ?;

-- name: RepointProposalIndex :exec
UPDATE OR IGNORE proposal_ingredients SET ingredient_id = sqlc.arg(winner_id) WHERE ingredient_id = sqlc.arg(loser_id);

-- name: DeleteProposalIndex :exec
DELETE FROM proposal_ingredients WHERE ingredient_id = ?;
