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
DELETE FROM source_snapshots WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND recipe_id IS NULL;

-- name: GetSnapshotByID :one
SELECT kind, content, sha256, created_at, created_by, parser_version FROM source_snapshots
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id);

-- The document of the draft whose source a snapshot was. A draft's snapshot
-- belongs to that one draft, and an accepted draft is never pruned (ADR-00035).
-- name: GetSnapshotDraftDocument :one
SELECT document FROM proposals
WHERE snapshot_id = sqlc.arg(snapshot_id) AND household_id = sqlc.arg(household_id) AND kind = 'draft'
ORDER BY id LIMIT 1;

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

-- A re-import draft (ADR-00042) names its recipe in the document and is not a
-- draft of a new recipe, so the bound on those does not count it.
-- name: CountPendingDrafts :one
SELECT COUNT(*) FROM proposals
WHERE household_id = ? AND kind = 'draft' AND state = 'pending' AND json_extract(document, '$.reimport') IS NULL;

-- The pending re-import drafts of a recipe. A recipe has at most one, kept by
-- the transaction that writes it, not by an index: the table's own check
-- gives a draft no recipe column (ADR-00042).
-- name: ListPendingReimports :many
SELECT id, token FROM proposals
WHERE household_id = sqlc.arg(household_id) AND kind = 'draft' AND state = 'pending'
  AND CAST(json_extract(document, '$.reimport.recipe') AS INTEGER) = CAST(sqlc.arg(recipe_id) AS INTEGER);

-- A proposal changes state only while it is pending.
-- name: SetProposalState :execrows
UPDATE proposals SET state = sqlc.arg(state), state_at = sqlc.arg(state_at)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND state = 'pending';

-- name: AcceptDraft :execrows
UPDATE proposals SET state = 'accepted', result_recipe_id = sqlc.arg(result_recipe_id), state_at = sqlc.arg(state_at)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND kind = 'draft' AND state = 'pending';

-- reason is left as it is when NULL: a person's edit on the review screen
-- keeps the reason an agent gave for its revision (ADR-00048).
-- name: UpdateDraftDocument :execrows
UPDATE proposals SET document = sqlc.arg(document), reason = COALESCE(sqlc.narg(reason), reason)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(household_id) AND kind = 'draft' AND state = 'pending';

-- name: ListPendingProposalAges :many
SELECT id, created_at FROM proposals WHERE state = 'pending';

-- name: ExpireProposal :execrows
UPDATE proposals SET state = 'expired', state_at = sqlc.arg(state_at) WHERE id = sqlc.arg(id) AND state = 'pending';

-- name: ListFinishedProposals :many
SELECT id, household_id, snapshot_id, state_at FROM proposals WHERE state IN ('rejected', 'superseded', 'expired');

-- name: DeleteProposal :exec
DELETE FROM proposals WHERE id = ?;

-- A new proposal for a recipe supersedes the pending one.
-- name: SetPendingEditSuperseded :execrows
UPDATE proposals SET state = 'superseded', state_at = sqlc.arg(state_at)
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) AND kind = 'edit' AND state = 'pending';

-- The household's pending ingestion drafts, re-imports included, oldest first,
-- for an agent to list and for an import's duplicate check (ADR-00048). The
-- name and source address are read from the draft document, where the draft
-- codec writes them.
-- name: ListPendingDrafts :many
SELECT token, CAST(COALESCE(json_extract(document, '$.name.v'), '') AS TEXT) AS name,
    CAST(COALESCE(json_extract(document, '$.source_url'), '') AS TEXT) AS source_url,
    CAST(json_extract(document, '$.reimport.recipe') IS NOT NULL AS INTEGER) AS reimport,
    reason, created_at
FROM proposals
WHERE household_id = sqlc.arg(household_id) AND kind = 'draft' AND state = 'pending'
ORDER BY created_at, id;
