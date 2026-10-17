-- Recipe list and search, scoped to the household. The FTS5 index
-- (recipe_search, migrations 00004 and 00008) is kept in step by triggers;
-- these queries only read it.

-- name: ListRecipeSummaries :many
SELECT id, name, description, rating, prep_ns, cook_ns, total_ns
FROM recipes WHERE household_id = ? ORDER BY lower(name), id;

-- The full-text query itself is hand-written in search.go: sqlc cannot resolve
-- the `recipe_search MATCH ?` form that FTS5 requires.

-- name: ListAllRecipeTags :many
SELECT recipe_id, tag FROM recipe_tags WHERE household_id = ? ORDER BY recipe_id, position;
