-- InsertProfile creates the profile of an email. An email that already has one
-- keeps it: the address of a profile never changes once it is shared.
-- name: InsertProfile :exec
INSERT INTO profiles (email, address, created_at, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (email) DO NOTHING;

-- name: GetProfileByEmail :one
SELECT email, address, display_name, avatar, created_at, updated_at
FROM profiles WHERE email = ?;

-- name: GetProfileByAddress :one
SELECT email, address, display_name, avatar, created_at, updated_at
FROM profiles WHERE address = ?;

-- name: SetDisplayName :execrows
UPDATE profiles SET display_name = sqlc.arg(display_name), updated_at = sqlc.arg(now)
WHERE email = sqlc.arg(email);
