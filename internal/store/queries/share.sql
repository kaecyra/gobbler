-- Recipe shares (ADR-00018, Sharing): a copy of a recipe sent from one
-- household to another. The row holds the document and snapshots as they were
-- at send time; nothing in it points at a row of the sender.

-- name: InsertRecipeShare :one
INSERT INTO recipe_shares (sender_household_id, recipient_household_id, sent_by, recipe_document, source_snapshot, created_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id;

-- ListPendingShares is the recipient household's inbox.
-- name: ListPendingShares :many
SELECT s.id, s.recipe_document, s.created_at, h.name AS sender_name
FROM recipe_shares s
JOIN households h ON h.id = s.sender_household_id
WHERE s.recipient_household_id = sqlc.arg(household_id) AND s.status = 'pending'
ORDER BY s.id;

-- GetShare reads one share addressed to the household, whatever its status.
-- name: GetShare :one
SELECT s.id, s.recipe_document, s.source_snapshot, s.status, s.created_at, h.name AS sender_name
FROM recipe_shares s
JOIN households h ON h.id = s.sender_household_id
WHERE s.id = sqlc.arg(id) AND s.recipient_household_id = sqlc.arg(household_id);

-- ResolveShare moves a pending share to accepted or declined. It is the claim
-- that makes a share usable once: of two concurrent resolutions one affects a row.
-- name: ResolveShare :execrows
UPDATE recipe_shares SET status = sqlc.arg(status), resolved_at = sqlc.arg(resolved_at)
WHERE id = sqlc.arg(id) AND recipient_household_id = sqlc.arg(household_id) AND status = 'pending';

-- ShareIngredient names an ingredient a household's recipe uses, with the
-- household that owns the entry (NULL for a central one). An entry outside the
-- household's scope is not returned.
-- name: ShareIngredient :one
SELECT name, household_id FROM ingredients
WHERE id = sqlc.arg(id) AND (household_id IS NULL OR household_id = sqlc.arg(scope));
