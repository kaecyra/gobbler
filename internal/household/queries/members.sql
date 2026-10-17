-- name: CountMembers :one
SELECT count(*) FROM household_members WHERE household_id = ?;

-- LeaveHousehold removes email unless it is the household's last member. The
-- count and the delete are one statement, so two members leaving at once
-- cannot both succeed and strand the household with nobody.
-- name: LeaveHousehold :one
DELETE FROM household_members
WHERE household_members.email = sqlc.arg(email)
  AND (SELECT count(*) FROM household_members m
       WHERE m.household_id = household_members.household_id) > 1
RETURNING household_id;

-- RemoveMember removes target from the household, on behalf of another member
-- of that same household. It removes nobody when removed_by is not a member of
-- the household or is the target.
-- name: RemoveMember :execrows
DELETE FROM household_members
WHERE household_members.household_id = sqlc.arg(household_id)
  AND household_members.email = sqlc.arg(target)
  AND household_members.email <> sqlc.arg(removed_by)
  AND EXISTS (SELECT 1 FROM household_members m
              WHERE m.household_id = sqlc.arg(household_id) AND m.email = sqlc.arg(removed_by));

-- RevokeMemberInvitations withdraws the join invitations a member issued into
-- a household and nobody has accepted yet. Run when the member leaves or is
-- removed: only the issuer can revoke an invitation, and they can no longer
-- reach the page that does it.
-- name: RevokeMemberInvitations :exec
UPDATE invitations SET revoked_at = sqlc.arg(now)
WHERE issued_by = sqlc.arg(issued_by) AND household_id = sqlc.arg(household_id) AND kind = 'join'
  AND accepted_at IS NULL AND revoked_at IS NULL;

-- TouchHousehold is a no-op write that fails fast on a missing household
-- before the reads that follow it.
-- name: TouchHousehold :execrows
UPDATE households SET name = name WHERE id = ?;

-- DeleteHousehold removes the household; every table that belongs to it
-- cascades from here.
-- name: DeleteHousehold :execrows
DELETE FROM households WHERE id = ?;

-- name: ListPendingJoinInvitations :many
SELECT i.id, i.kind, i.email, i.household_id, i.issued_by, i.created_at, i.expires_at
FROM invitations i
WHERE i.household_id = sqlc.arg(household_id) AND i.kind = 'join'
  AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > sqlc.arg(now)
ORDER BY i.id;

-- name: ListPendingHouseholdInvitations :many
SELECT i.id, i.kind, i.email, i.household_id, i.issued_by, i.created_at, i.expires_at
FROM invitations i
WHERE i.kind = 'household' AND i.issued_by = sqlc.arg(issued_by)
  AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > sqlc.arg(now)
ORDER BY i.id;

-- GetInvitationFor reads an invitation only if it is addressed to email, with
-- the name of the household a join invitation leads to.
-- name: GetInvitationFor :one
SELECT i.id, i.kind, i.email, i.household_id, i.issued_by, i.created_at, i.expires_at,
       i.accepted_at, i.revoked_at, COALESCE(h.name, '') AS household_name
FROM invitations i
LEFT JOIN households h ON h.id = i.household_id
WHERE i.id = sqlc.arg(id) AND i.email = sqlc.arg(email);

-- InsertJoinInvitation issues a join invitation only while the issuer is a
-- member of the household, in the one statement, so an invitation racing the
-- issuer's own removal cannot leave a live invitation from a departed member.
-- name: InsertJoinInvitation :one
INSERT INTO invitations (kind, email, household_id, issued_by, created_at, expires_at)
SELECT 'join', sqlc.arg(email), sqlc.arg(household_id), sqlc.arg(issued_by), sqlc.arg(created_at), sqlc.arg(expires_at)
WHERE EXISTS (SELECT 1 FROM household_members m
              WHERE m.household_id = sqlc.arg(household_id) AND m.email = sqlc.arg(issued_by))
RETURNING id;
