-- IngredientVisible reports whether an ingredient is one the household may
-- reference: a central entry or an entry of its own overlay (ADR-00018). An id
-- that does not exist is not visible either.
-- name: IngredientVisible :one
SELECT EXISTS (
    SELECT 1 FROM ingredients
    WHERE id = sqlc.arg(id) AND (household_id IS NULL OR household_id = sqlc.arg(scope))
);
