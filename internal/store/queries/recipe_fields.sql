-- The optional fields ADR-00022 added to the recipe document and to notes.
-- Like the video ID (video.sql), they are written by statements of their own so
-- the document queries stay as they were; every one still filters on the
-- household.

-- name: SetRecipeFields :exec
UPDATE recipes SET meal_type = ?, spice_level = ? WHERE id = ? AND household_id = ?;

-- name: GetRecipeFields :one
SELECT meal_type, spice_level FROM recipes WHERE id = ? AND household_id = ?;

-- name: SetStepFields :exec
UPDATE steps SET stage_kind = ?, title = ? WHERE id = ? AND household_id = ?;

-- name: ListStepFields :many
SELECT id, stage_kind, title FROM steps WHERE recipe_id = ? AND household_id = ?;

-- name: SetNotePinned :execrows
UPDATE notes SET pinned = ? WHERE id = ? AND recipe_id = ? AND household_id = ?;

-- name: ListPinnedNoteIDs :many
SELECT id FROM notes WHERE recipe_id = ? AND household_id = ? AND pinned = 1;
