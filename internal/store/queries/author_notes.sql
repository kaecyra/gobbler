-- The recipe author's notes (ADR-00041). Like the video ID and the optional
-- fields, they have statements of their own so the document queries stay as
-- they were; every one filters on the household.

-- name: InsertAuthorNote :exec
INSERT INTO recipe_author_notes (household_id, recipe_id, position, title, text) VALUES (?, ?, ?, ?, ?);

-- name: DeleteAuthorNotes :exec
DELETE FROM recipe_author_notes WHERE recipe_id = ? AND household_id = ?;

-- name: ListAuthorNotes :many
SELECT title, text FROM recipe_author_notes WHERE recipe_id = ? AND household_id = ? ORDER BY position;
