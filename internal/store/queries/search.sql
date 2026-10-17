-- Recipe list and search, scoped to the household. The FTS5 index
-- (recipe_search, migrations 00004 and 00008) is kept in step by triggers;
-- these queries only read it.

-- name: ListRecipeSummaries :many
SELECT id, name, description, rating, prep_ns, cook_ns, total_ns
FROM recipes WHERE household_id = ? ORDER BY lower(name), id;

-- The full-text query itself is hand-written in search.go: sqlc cannot resolve
-- the `recipe_search MATCH ?` form that FTS5 requires.

-- Recipes carrying every one of the given tags.
-- name: ListRecipeIDsWithAllTags :many
SELECT recipe_id FROM recipe_tags
WHERE tag IN (sqlc.slice('tags')) AND household_id = ?
GROUP BY recipe_id
HAVING COUNT(DISTINCT tag) = CAST(? AS INTEGER);

-- name: ListAllRecipeTags :many
SELECT recipe_id, tag FROM recipe_tags WHERE household_id = ? ORDER BY recipe_id, position;
