-- name: GetAvatarJob :one
SELECT email, id, state, crop_x, crop_y, crop_w, crop_h, error, created_at, updated_at
FROM avatar_jobs WHERE email = ?;

-- UpsertAvatarJob records a new upload in place of any earlier one, unless the
-- earlier one is queued: a job that is about to run is not replaced under it.
-- Zero rows affected means it was queued.
-- name: UpsertAvatarJob :execrows
INSERT INTO avatar_jobs (email, id, state, created_at, updated_at)
VALUES (sqlc.arg(email), sqlc.arg(id), 'uploaded', sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (email) DO UPDATE SET
    id = excluded.id, state = 'uploaded', crop_x = 0, crop_y = 0, crop_w = 1, crop_h = 1,
    error = '', created_at = excluded.created_at, updated_at = excluded.updated_at
WHERE avatar_jobs.state <> 'queued';

-- name: QueueAvatarJob :execrows
UPDATE avatar_jobs
SET state = 'queued', crop_x = sqlc.arg(crop_x), crop_y = sqlc.arg(crop_y),
    crop_w = sqlc.arg(crop_w), crop_h = sqlc.arg(crop_h), error = '', updated_at = sqlc.arg(now)
WHERE email = sqlc.arg(email) AND id = sqlc.arg(id) AND state IN ('uploaded', 'failed');

-- name: FailAvatarJob :exec
UPDATE avatar_jobs SET state = 'failed', error = sqlc.arg(error), updated_at = sqlc.arg(now)
WHERE email = sqlc.arg(email) AND id = sqlc.arg(id);

-- name: DeleteAvatarJob :exec
DELETE FROM avatar_jobs WHERE email = ? AND id = ?;

-- name: ListQueuedAvatarJobs :many
SELECT email, id, state, crop_x, crop_y, crop_w, crop_h, error, created_at, updated_at
FROM avatar_jobs WHERE state = 'queued' ORDER BY created_at, email;

-- name: SetAvatar :execrows
UPDATE profiles SET avatar = sqlc.arg(avatar), updated_at = sqlc.arg(now)
WHERE email = sqlc.arg(email);
