-- Ingredients come in two layers (ADR-00018): central rows have a NULL
-- household_id, a household's overlay rows carry its id. A query that takes
-- sqlc.arg(scope) reads the central layer plus the overlay of that household;
-- scope 0 matches no household, so it reads the central layer alone.

-- name: CreateIngredient :one
INSERT INTO ingredients (household_id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed)
VALUES (sqlc.narg(household_id), sqlc.arg(name), sqlc.arg(aisle), sqlc.arg(density_g_per_ml), sqlc.arg(usda_fdc_id), sqlc.arg(reviewed))
RETURNING id;

-- name: UpdateIngredient :execrows
UPDATE ingredients
SET name = sqlc.arg(name), aisle = sqlc.arg(aisle), density_g_per_ml = sqlc.arg(density_g_per_ml),
    usda_fdc_id = sqlc.arg(usda_fdc_id), reviewed = sqlc.arg(reviewed)
WHERE id = sqlc.arg(id) AND household_id IS sqlc.narg(household_id);

-- name: GetIngredient :one
SELECT id, household_id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed, suggested_at
FROM ingredients
WHERE id = sqlc.arg(id) AND (household_id IS NULL OR household_id = sqlc.arg(scope));

-- name: ListIngredients :many
SELECT id, household_id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed, suggested_at
FROM ingredients
WHERE household_id IS NULL OR household_id = sqlc.arg(scope)
ORDER BY name;

-- name: ListUnreviewedIngredients :many
SELECT id, household_id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed, suggested_at
FROM ingredients
WHERE reviewed = 0 AND (household_id IS NULL OR household_id = sqlc.arg(scope))
ORDER BY name;

-- LookupOverlayIngredientID finds a term among the household's own entries and
-- its own aliases, including its aliases for central entries.
-- name: LookupOverlayIngredientID :one
SELECT i.id FROM ingredients i WHERE i.household_id = sqlc.arg(owner) AND i.name = sqlc.arg(term)
UNION ALL
SELECT a.ingredient_id FROM ingredient_aliases a WHERE a.household_id = sqlc.arg(owner) AND a.alias = sqlc.arg(term)
ORDER BY 1 LIMIT 1;

-- name: LookupCentralIngredientID :one
SELECT id FROM ingredients WHERE household_id IS NULL AND name = sqlc.arg(term)
UNION ALL
SELECT ingredient_id FROM ingredient_aliases WHERE household_id IS NULL AND alias = sqlc.arg(term)
ORDER BY 1 LIMIT 1;

-- name: ListIngredientAliases :many
SELECT alias FROM ingredient_aliases
WHERE ingredient_id = sqlc.arg(ingredient_id) AND (household_id IS NULL OR household_id = sqlc.arg(scope))
ORDER BY alias;

-- name: ListAllIngredientAliases :many
SELECT ingredient_id, alias FROM ingredient_aliases
WHERE household_id IS NULL OR household_id = sqlc.arg(scope)
ORDER BY alias;

-- DeleteIngredientAliases clears the aliases of one layer of an entry: the
-- central ones when household_id is NULL, otherwise that household's.
-- name: DeleteIngredientAliases :exec
DELETE FROM ingredient_aliases
WHERE ingredient_id = sqlc.arg(ingredient_id) AND household_id IS sqlc.narg(household_id);

-- name: AddIngredientAlias :exec
INSERT INTO ingredient_aliases (household_id, alias, ingredient_id)
VALUES (sqlc.narg(household_id), sqlc.arg(alias), sqlc.arg(ingredient_id));

-- name: DeleteIngredient :execrows
DELETE FROM ingredients WHERE id = ?;

-- name: MoveIngredientAliases :exec
UPDATE ingredient_aliases
SET ingredient_id = sqlc.arg(winner_id)
WHERE ingredient_id = sqlc.arg(loser_id);

-- LockIngredient takes the database write lock with a no-op write, so the
-- reads that follow in the same transaction see the state it will change.
-- name: LockIngredient :execrows
UPDATE ingredients SET reviewed = reviewed WHERE id = ?;

-- name: CarryIngredientData :exec
UPDATE ingredients
SET density_g_per_ml = COALESCE(density_g_per_ml, sqlc.narg(density_g_per_ml)),
    usda_fdc_id = COALESCE(usda_fdc_id, sqlc.narg(usda_fdc_id))
WHERE id = sqlc.arg(id);

-- name: GetAisleOverride :one
SELECT aisle FROM ingredient_aisle_overrides
WHERE household_id = ? AND ingredient_id = ?;

-- name: ListAisleOverrides :many
SELECT ingredient_id, aisle FROM ingredient_aisle_overrides WHERE household_id = ?;

-- name: SetAisleOverride :exec
INSERT INTO ingredient_aisle_overrides (household_id, ingredient_id, aisle) VALUES (?, ?, ?)
ON CONFLICT (household_id, ingredient_id) DO UPDATE SET aisle = excluded.aisle;

-- name: DeleteAisleOverride :exec
DELETE FROM ingredient_aisle_overrides WHERE household_id = ? AND ingredient_id = ?;

-- name: SuggestIngredient :execrows
UPDATE ingredients SET suggested_at = sqlc.arg(suggested_at)
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(owner);

-- name: UnsuggestIngredient :execrows
UPDATE ingredients SET suggested_at = NULL
WHERE id = sqlc.arg(id) AND household_id = sqlc.arg(owner);

-- ListSuggestedIngredients is the operator's review queue. It selects no
-- household: the operator sees what is suggested, not who suggested it.
-- name: ListSuggestedIngredients :many
SELECT id, household_id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed, suggested_at
FROM ingredients
WHERE suggested_at IS NOT NULL AND household_id IS NOT NULL
ORDER BY suggested_at, name;

-- name: ListSuggestedIngredientAliases :many
SELECT a.ingredient_id, a.alias FROM ingredient_aliases a
JOIN ingredients i ON i.id = a.ingredient_id
WHERE i.suggested_at IS NOT NULL AND i.household_id IS NOT NULL AND a.household_id = i.household_id
ORDER BY a.alias;

-- ListPromotionDuplicates returns the overlay entries, of any household, that
-- duplicate ingredient id: the entry's normalised name or one of its own
-- aliases equals its name or one of its aliases (ADR-00018).
-- name: ListPromotionDuplicates :many
SELECT d.id, d.household_id, d.name, d.aisle FROM ingredients d
WHERE d.household_id IS NOT NULL AND d.id <> sqlc.arg(id) AND (
    d.name IN (SELECT e.name FROM ingredients e WHERE e.id = sqlc.arg(id)
               UNION SELECT a.alias FROM ingredient_aliases a WHERE a.ingredient_id = sqlc.arg(id))
    OR d.id IN (SELECT da.ingredient_id FROM ingredient_aliases da
                WHERE da.household_id = d.household_id AND da.ingredient_id = d.id AND da.alias IN
                    (SELECT e.name FROM ingredients e WHERE e.id = sqlc.arg(id)
                     UNION SELECT a.alias FROM ingredient_aliases a WHERE a.ingredient_id = sqlc.arg(id))))
ORDER BY d.id;

-- name: PromoteIngredient :execrows
UPDATE ingredients SET household_id = NULL, suggested_at = NULL, reviewed = 1
WHERE id = ? AND household_id IS NOT NULL;

-- name: PromoteIngredientAliases :exec
UPDATE ingredient_aliases SET household_id = NULL WHERE ingredient_id = ?;

-- GetOverlayIngredient reads an overlay entry whichever household holds it.
-- Only the operator's promotion uses it.
-- name: GetOverlayIngredient :one
SELECT id, household_id, name, aisle, density_g_per_ml, usda_fdc_id, reviewed, suggested_at
FROM ingredients WHERE id = ? AND household_id IS NOT NULL;

-- name: ListOverlayIngredientAliases :many
SELECT alias FROM ingredient_aliases WHERE ingredient_id = ? AND household_id IS NOT NULL ORDER BY alias;

-- PruneRedundantAliases drops a household's aliases on an entry that the
-- entry's own name or central aliases already cover.
-- name: PruneRedundantAliases :exec
DELETE FROM ingredient_aliases
WHERE ingredient_aliases.household_id = sqlc.arg(owner) AND ingredient_aliases.ingredient_id = sqlc.arg(ingredient_id)
  AND (ingredient_aliases.alias = (SELECT name FROM ingredients WHERE id = sqlc.arg(ingredient_id))
       OR ingredient_aliases.alias IN (SELECT c.alias FROM ingredient_aliases c
                    WHERE c.household_id IS NULL AND c.ingredient_id = sqlc.arg(ingredient_id)));
