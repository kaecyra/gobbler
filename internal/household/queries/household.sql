-- name: InsertHousehold :one
INSERT INTO households (name, created_at) VALUES (?, ?) RETURNING id;

-- name: GetHouseholdByMemberEmail :one
SELECT h.id, h.name, h.created_at
FROM households h
JOIN household_members m ON m.household_id = h.id
WHERE m.email = ?;

-- name: InsertMember :exec
INSERT INTO household_members (household_id, email, joined_at) VALUES (?, ?, ?);

-- name: GetHouseholdByID :one
SELECT id, name, created_at FROM households WHERE id = ?;

-- name: ListMemberEmails :many
SELECT email FROM household_members WHERE household_id = ? ORDER BY id;

-- name: InsertInvitation :one
INSERT INTO invitations (kind, email, household_id, issued_by, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING id;

-- name: GetInvitation :one
SELECT id, kind, email, household_id, issued_by, created_at, expires_at, accepted_at, revoked_at
FROM invitations WHERE id = ?;

-- ClaimInvitation marks an invitation accepted only while it is addressed to
-- email, unused, unrevoked and unexpired, so single use holds under
-- concurrent accepts. It runs first in the accepting transaction so a failed
-- claim ends the accept before anything else is read or written.
-- name: ClaimInvitation :one
UPDATE invitations SET accepted_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND email = sqlc.arg(email)
  AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > sqlc.arg(now)
RETURNING kind, household_id;

-- RevokeInvitation withdraws an invitation its issuer has not yet seen
-- accepted.
-- name: RevokeInvitation :execrows
UPDATE invitations SET revoked_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND issued_by = sqlc.arg(issued_by)
  AND accepted_at IS NULL AND revoked_at IS NULL;
