package web

import (
	"net/http"

	"github.com/kaecyra/gobbler/internal/web/recipes"
)

// homeRoute is the home page: the hero search, a suggestion and the recently
// made panel (package recipes).
func homeRoute(d Deps) http.HandlerFunc {
	return recipes.Home(recipes.Deps{Library: d.Recipes, Pictures: recipePictures(d)})
}
