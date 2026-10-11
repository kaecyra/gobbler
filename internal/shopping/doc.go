// Package shopping builds a shopping list from recipes and manual items
// (ADR-00009) and exports it as text.
//
// This part of the package is pure: Generate takes values and returns values.
// It reads no database, clock or network and imports only internal/quantity
// and internal/recipe, so persistence, check state, the live view and the
// Todoist push live elsewhere. Catalog facts (name, aisle, density, pantry
// membership) arrive as plain values in Input rather than through a store.
//
// Generate scales each recipe's lines by its multiplier with quantity.Scale,
// drops optional lines unless Input.IncludeOptional is set, and sets aside
// pantry ingredients in List.Pantry (Input.AddBack puts one back). Lines merge
// by canonical ingredient. Amounts in one dimension sum exactly into one
// amount; volume joins mass only when the ingredient has a density; amounts
// that cannot combine ("2 cups" and "3 cloves") stay separate on the one item.
// Packaged amounts ("1 (14 oz) can") sum only with the same package size.
// Items group by aisle: aisles alphabetically, then Uncategorised last, items
// alphabetically within each. The same input always yields the same output.
//
// A line with no resolved ingredient (ID zero) becomes its own free-text item
// named by its original line, so a recipe the owner has not finished cleaning
// up still produces a usable list. Its text cannot be parsed or rescaled, so
// such lines are never merged, and the multiplier is kept on Item.Scale and
// shown by Text as "(x2)"; nothing is silently undercounted. Manual free-text
// items merge by text and keep their amounts. A resolved ingredient that
// Input.Catalog does not know is an error.
package shopping
