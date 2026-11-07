-- name: CountIngredientLines :one
SELECT COUNT(*) FROM ingredient_lines WHERE ingredient_id = ?;

-- CountRecipesByIngredient is, for every ingredient a household's recipes use,
-- how many of its recipes use it. A recipe naming the ingredient on several
-- lines counts once. Unresolved lines (NULL ingredient) are left out.
-- name: CountRecipesByIngredient :many
SELECT l.ingredient_id AS ingredient_id, COUNT(DISTINCT c.recipe_id) AS recipes
FROM ingredient_lines l
JOIN components c ON c.household_id = l.household_id AND c.id = l.component_id
WHERE l.household_id = ? AND l.ingredient_id IS NOT NULL
GROUP BY l.ingredient_id;
