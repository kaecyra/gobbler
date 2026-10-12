-- name: CountIngredientLines :one
SELECT COUNT(*) FROM ingredient_lines WHERE ingredient_id = ?;
