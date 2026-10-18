-- The library page and its side panels read a card's parts for every recipe of
-- a household at once, so a page costs a fixed number of queries however many
-- recipes it lists. Every query takes the household and filters on it
-- (ADR-00018).

-- name: ListLibraryFields :many
SELECT id, meal_type, spice_level, source_url, author FROM recipes WHERE household_id = ?;

-- name: ListLibraryComponents :many
SELECT recipe_id, name FROM components WHERE household_id = ? AND name <> '' ORDER BY recipe_id, position;

-- The ingredient names of the required lines, in recipe order. A line that
-- names no catalog ingredient has nothing to show.
-- name: ListLibraryIngredients :many
SELECT c.recipe_id, i.name
FROM ingredient_lines l
JOIN components c ON c.id = l.component_id
JOIN ingredients i ON i.id = l.ingredient_id
WHERE l.household_id = ? AND l.optional = 0
ORDER BY c.recipe_id, c.position, l.position;

-- name: ListLibrarySteps :many
SELECT s.recipe_id, s.duration_ns, s.passive, s.stage_kind
FROM steps s JOIN components c ON c.id = s.component_id
WHERE s.household_id = ? ORDER BY s.recipe_id, c.position, s.position;

-- name: ListLibraryMadeStats :many
SELECT recipe_id, COUNT(*) AS made, CAST(MAX(made_on) AS TEXT) AS last_made FROM made_it
WHERE household_id = ? GROUP BY recipe_id;

-- name: ListRecentMadeIt :many
SELECT m.recipe_id, r.name, m.made_on, m.comment, m.rating
FROM made_it m JOIN recipes r ON r.id = m.recipe_id AND r.household_id = m.household_id
WHERE m.household_id = sqlc.arg(household_id)
ORDER BY m.made_on DESC, m.id DESC LIMIT sqlc.arg(max_rows);

-- name: ListLibraryCuisines :many
SELECT id, cuisine FROM recipes WHERE household_id = ?;
