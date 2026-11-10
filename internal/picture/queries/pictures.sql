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
INSERT INTO draft_candidates (household_id, proposal_id, offer, position, source_url, key, width, height)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListCandidates :many
SELECT offer, position, source_url, key, width, height FROM draft_candidates
WHERE proposal_id = ? AND household_id = ? ORDER BY position;

-- name: DeleteCandidates :exec
DELETE FROM draft_candidates WHERE proposal_id = ? AND household_id = ?;

-- name: ListRecipeObjectKeys :many
SELECT rp.key FROM recipe_pictures rp WHERE rp.recipe_id = sqlc.arg(recipe) AND rp.household_id = sqlc.arg(household)
UNION ALL
SELECT c.key FROM draft_candidates c
JOIN proposals p ON p.household_id = c.household_id AND p.id = c.proposal_id
WHERE c.household_id = sqlc.arg(household)
  AND (p.recipe_id = sqlc.arg(recipe) OR p.result_recipe_id = sqlc.arg(recipe))
UNION ALL
SELECT f.key FROM recipe_found_pictures f WHERE f.recipe_id = sqlc.arg(recipe) AND f.household_id = sqlc.arg(household);

-- name: ListHouseholdObjectKeys :many
SELECT rp.key FROM recipe_pictures rp WHERE rp.household_id = sqlc.arg(household)
UNION ALL
SELECT c.key FROM draft_candidates c WHERE c.household_id = sqlc.arg(household)
UNION ALL
SELECT f.key FROM recipe_found_pictures f WHERE f.household_id = sqlc.arg(household);

-- GetDraftProposal proves a proposal is a draft of the household before any
-- object is written under its id.
-- name: GetDraftProposal :one
SELECT id FROM proposals WHERE id = ? AND household_id = ? AND kind = 'draft';

-- Pictures offered for a saved recipe on its edit page (ADR-00051), each
-- named by its offer ID.

-- name: InsertFoundPicture :exec
INSERT INTO recipe_found_pictures (household_id, recipe_id, offer, kind, source_url, key, width, height, found_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- ListFoundPictures returns the recipe's offered pictures found at or after
-- the cutoff, found ones first in the order the search found them: an older
-- one has expired and waits for the sweep.
-- name: ListFoundPictures :many
SELECT offer, kind, source_url, key, width, height FROM recipe_found_pictures
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) AND found_at >= sqlc.arg(cutoff)
ORDER BY kind = 'upload', id;

-- ListFoundPictureKeys returns the keys of the recipe's offered pictures,
-- expired ones included.
-- name: ListFoundPictureKeys :many
SELECT key FROM recipe_found_pictures WHERE recipe_id = ? AND household_id = ?;

-- name: ListFoundPictureKeysOfKind :many
SELECT key FROM recipe_found_pictures WHERE recipe_id = ? AND household_id = ? AND kind = ?;

-- name: DeleteFoundPictures :exec
DELETE FROM recipe_found_pictures WHERE recipe_id = ? AND household_id = ?;

-- name: DeleteFoundPicturesOfKind :exec
DELETE FROM recipe_found_pictures WHERE recipe_id = ? AND household_id = ? AND kind = ?;

-- The sweep (ADR-00051): every household's offered pictures found before the
-- cutoff, a recipe at a time, so the sweep can hold that recipe's lock.
-- name: ListRecipesWithExpiredFoundPictures :many
SELECT DISTINCT recipe_id FROM recipe_found_pictures WHERE found_at < ? ORDER BY recipe_id;

-- name: ListExpiredFoundPictureKeys :many
SELECT key FROM recipe_found_pictures WHERE recipe_id = ? AND found_at < ?;

-- name: DeleteExpiredFoundPictures :execrows
DELETE FROM recipe_found_pictures WHERE recipe_id = ? AND found_at < ?;
