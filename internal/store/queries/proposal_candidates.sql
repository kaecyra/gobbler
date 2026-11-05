-- The object keys of a draft's picture candidates (ADR-00039), read by Sweep
-- before it deletes a finished proposal; the rows cascade away with it.
-- internal/picture owns the table's other statements.

-- name: ListProposalCandidateKeys :many
SELECT key FROM draft_candidates WHERE household_id = ? AND proposal_id = ?;
