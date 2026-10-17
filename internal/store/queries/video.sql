-- The YouTube video of a recipe and the thumbnails kept for videos (ADR-00021).
-- The video ID is a column of recipes written by its own statements so the
-- document queries stay as they were; a recipe still goes through the household
-- filter on every one.

-- name: SetRecipeVideo :exec
UPDATE recipes SET video_id = ? WHERE id = ? AND household_id = ?;

-- name: GetRecipeVideo :one
SELECT video_id FROM recipes WHERE id = ? AND household_id = ?;

-- InsertVideoThumbnail keeps the first download of an ID; a second save of a
-- recipe naming the same video does not rewrite the row.
-- name: InsertVideoThumbnail :exec
INSERT INTO video_thumbnails (video_id, content, content_type, fetched_at) VALUES (?, ?, ?, ?)
ON CONFLICT (video_id) DO NOTHING;

-- name: HasVideoThumbnail :one
SELECT EXISTS (SELECT 1 FROM video_thumbnails WHERE video_id = ?);

-- GetRecipeThumbnail reaches a thumbnail only through a recipe of the household
-- that names the video: never by video ID alone.
-- name: GetRecipeThumbnail :one
SELECT t.content, t.content_type FROM recipes r
JOIN video_thumbnails t ON t.video_id = r.video_id
WHERE r.id = ? AND r.household_id = ?;
