-- The proposal an accepted edit came from, for history (ADR-00035): the
-- revisions that were written by accepting one, and the proposal that produced
-- the current version.

-- name: ListRevisionProposals :many
SELECT r.version, p.token, p.reason, p.tool, p.proposed_by, p.credential_kind, p.credential_id, p.credential_ref,
    p.credential_owner, p.credential_created_at, p.created_at
FROM revisions r JOIN proposals p ON p.household_id = r.household_id AND p.id = r.proposal_id
WHERE r.recipe_id = sqlc.arg(recipe_id) AND r.household_id = sqlc.arg(household_id) AND r.change_kind = 'proposal';

-- Accepting a proposal made against version v produced version v+1, and a
-- version is produced once, so this names the proposal behind a version.
-- name: GetAcceptedEditProposalFor :one
SELECT token, reason, tool, proposed_by, credential_kind, credential_id, credential_ref, credential_owner,
    credential_created_at, created_at
FROM proposals
WHERE recipe_id = sqlc.arg(recipe_id) AND household_id = sqlc.arg(household_id) AND kind = 'edit' AND state = 'accepted'
    AND base_version = sqlc.arg(base_version);
