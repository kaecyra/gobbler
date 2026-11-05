-- Recipe pictures and the candidates fetched for drafts (ADR-00039). Every
-- statement filters on the household.

-- GetRecipeSource proves the recipe is the household's and returns the page it
-- was imported from, which relative image addresses resolve against.
-- name: GetRecipeSource :one
SELECT source_url FROM recipes WHERE id = ? AND household_id = ?;

-- name: GetPictureKey :one
SELECT key FROM recipe_pictures WHERE recipe_id = ? AND household_id = ?;

-- name: SetPictureKey :exec
INSERT INTO recipe_pictures (recipe_id, household_id, key, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (recipe_id) DO UPDATE SET key = excluded.key, updated_at = excluded.updated_at;

-- name: DeletePictureKey :execrows
DELETE FROM recipe_pictures WHERE recipe_id = ? AND household_id = ?;

-- name: InsertCandidate :exec
INSERT INTO draft_candidates (household_id, proposal_id, position, source_url, key, width, height)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListCandidates :many
SELECT position, source_url, key, width, height FROM draft_candidates
WHERE proposal_id = ? AND household_id = ? ORDER BY position;

-- name: DeleteCandidates :exec
DELETE FROM draft_candidates WHERE proposal_id = ? AND household_id = ?;

-- name: ListRecipeObjectKeys :many
SELECT rp.key FROM recipe_pictures rp WHERE rp.recipe_id = sqlc.arg(recipe) AND rp.household_id = sqlc.arg(household)
UNION ALL
SELECT c.key FROM draft_candidates c
JOIN proposals p ON p.household_id = c.household_id AND p.id = c.proposal_id
WHERE c.household_id = sqlc.arg(household)
  AND (p.recipe_id = sqlc.arg(recipe) OR p.result_recipe_id = sqlc.arg(recipe));

-- name: ListHouseholdObjectKeys :many
SELECT rp.key FROM recipe_pictures rp WHERE rp.household_id = sqlc.arg(household)
UNION ALL
SELECT c.key FROM draft_candidates c WHERE c.household_id = sqlc.arg(household);
