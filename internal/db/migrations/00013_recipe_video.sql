-- +goose Up
-- An optional YouTube video on a recipe (ADR-00021): the 11-character video ID
-- and nothing else about the video. Empty means no video, which is every recipe
-- stored before this migration. The ID is part of the recipe document, so a
-- revision, a restore and a shared copy carry it (ADR-00004).
ALTER TABLE recipes ADD COLUMN video_id TEXT NOT NULL DEFAULT '';

-- The preview image of a video, downloaded by the server when a recipe naming
-- the video is saved. One row per video ID serves every recipe and revision
-- that names it, so the table has no household column and is read only by
-- primary key, through a recipe of the requesting household (ADR-00021
-- decisions 3 and 4). A row is never rewritten; fetched_at says when it was
-- downloaded.
CREATE TABLE video_thumbnails (
    video_id     TEXT PRIMARY KEY CHECK (length(video_id) = 11),
    content      BLOB NOT NULL CHECK (length(content) > 0),
    content_type TEXT NOT NULL,
    fetched_at   TEXT NOT NULL
);

-- +goose Down
DROP TABLE video_thumbnails;
ALTER TABLE recipes DROP COLUMN video_id;
