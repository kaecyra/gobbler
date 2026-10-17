package recipeview

import "testing"

func TestAddToShoppingListPostsTheRecipeAtThePagesMultiplier(t *testing.T) {
	_, body := serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7")
	mustContain(t, body,
		`<form method="post" action="/shopping/add"`,
		`name="recipe" value="7"`,
		`name="multiplier" value="1"`,
		`data-testid="shopping-add"`)
	_, body = serve(t, fakeRecipes{7: testRecipe(t)}, "/recipes/7?scale=2")
	mustContain(t, body, `name="multiplier" value="2"`)
}
