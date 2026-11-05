-- The proposals that accepted edits came from, for history (ADR-00035): the
-- revisions that were written by accepting one.

-- name: ListRevisionProposals :many
SELECT r.version, p.token, p.reason, p.tool, p.proposed_by, p.credential_kind, p.credential_id, p.credential_ref,
    p.credential_owner, p.credential_created_at, p.created_at
FROM revisions r JOIN proposals p ON p.household_id = r.household_id AND p.id = r.proposal_id
WHERE r.recipe_id = sqlc.arg(recipe_id) AND r.household_id = sqlc.arg(household_id) AND r.change_kind = 'proposal';
