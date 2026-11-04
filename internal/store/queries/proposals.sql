-- Proposals (ADR-00035): whole documents awaiting a decision, and the source
-- snapshots of the drafts among them. Nothing here is read by a query over
-- live recipes.

-- name: InsertUnattachedSnapshot :one
INSERT INTO source_snapshots (household_id, kind, content, sha256, created_at, created_by, parser_version)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- A snapshot is attached once: only a row that has no recipe takes one.
-- name: AttachSnapshot :execrows
UPDATE source_snapshots SET recipe_id = sqlc.arg(recipe_id)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND recipe_id IS NULL;

-- Only a snapshot with no recipe can be deleted; an attached one goes with its
-- recipe.
-- name: DeleteUnattachedSnapshot :execrows
DELETE FROM source_snapshots WHERE id = ? AND recipe_id IS NULL;

-- name: GetSnapshotByID :one
SELECT kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id);

-- name: InsertProposal :one
INSERT INTO proposals (household_id, token, kind, state, recipe_id, base_version, snapshot_id, document, reason,
    proposed_by, credential_kind, credential_id, credential_ref, credential_owner, credential_created_at, tool, created_at, state_at)
VALUES (?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: InsertProposalIngredient :exec
INSERT INTO proposal_ingredients (household_id, proposal_id, ingredient_id) VALUES (?, ?, ?);

-- name: DeleteProposalIngredients :exec
DELETE FROM proposal_ingredients WHERE proposal_id = ?;

-- name: GetProposal :one
SELECT id, token, kind, state, recipe_id, base_version, result_recipe_id, snapshot_id, document, reason,
    proposed_by, credential_kind, credential_id, credential_ref, credential_owner, credential_created_at, tool, created_at, state_at
FROM proposals WHERE token = sqlc.arg(token) AND household_id = sqlc.arg(household_id);

-- name: GetPendingEditProposal :one
SELECT token FROM proposals
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) AND kind = 'edit' AND state = 'pending';

-- name: CountPendingDrafts :one
SELECT COUNT(*) FROM proposals WHERE household_id = ? AND kind = 'draft' AND state = 'pending';

-- A proposal changes state only while it is pending.
-- name: SetProposalState :execrows
UPDATE proposals SET state = sqlc.arg(state), state_at = sqlc.arg(state_at)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND state = 'pending';

-- name: AcceptDraft :execrows
UPDATE proposals SET state = 'accepted', result_recipe_id = sqlc.arg(result_recipe_id), state_at = sqlc.arg(state_at)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND kind = 'draft' AND state = 'pending';

-- name: UpdateDraftDocument :execrows
UPDATE proposals SET document = sqlc.arg(document)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND kind = 'draft' AND state = 'pending';

-- name: ListPendingProposalAges :many
SELECT id, created_at FROM proposals WHERE state = 'pending';

-- name: ExpireProposal :execrows
UPDATE proposals SET state = 'expired', state_at = sqlc.arg(state_at) WHERE id = sqlc.arg(id) AND state = 'pending';

-- name: ListFinishedProposals :many
SELECT id, snapshot_id, state_at FROM proposals WHERE state IN ('rejected', 'superseded', 'expired');

-- name: DeleteProposal :exec
DELETE FROM proposals WHERE id = ?;

-- A new proposal for a recipe supersedes the pending one.
-- name: SetPendingEditSuperseded :execrows
UPDATE proposals SET state = 'superseded', state_at = sqlc.arg(state_at)
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) AND kind = 'edit' AND state = 'pending';
