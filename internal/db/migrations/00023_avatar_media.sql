-- +goose Up
-- Avatar uploads in flight (ADR-00033). A person has at most one: the original
-- and its preview sit in media storage under avatars/uploads/<id> until
-- processing succeeds, and the row is deleted then. profiles.avatar (00020)
-- holds the key of the processed WebP. The profile package owns the SQL
-- (internal/profile).
--
-- state is uploaded (decoded and stored, waiting for the crop), queued (the crop
-- is chosen and the job runs in the background; a restart finds these and runs
-- them again) or failed (error says why; the original is kept so the person can
-- crop again). crop_* are fractions of the upright picture, from the top left.
-- id is 16 random bytes in unpadded base64url, like a profile address.
-- Timestamps are RFC 3339 UTC text.

CREATE TABLE avatar_jobs (
    email      TEXT NOT NULL PRIMARY KEY REFERENCES profiles (email) ON DELETE CASCADE,
    id         TEXT NOT NULL CHECK (length(id) = 22),
    state      TEXT NOT NULL CHECK (state IN ('uploaded', 'queued', 'failed')),
    crop_x     REAL NOT NULL DEFAULT 0,
    crop_y     REAL NOT NULL DEFAULT 0,
    crop_w     REAL NOT NULL DEFAULT 1,
    crop_h     REAL NOT NULL DEFAULT 1,
    error      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE avatar_jobs;
