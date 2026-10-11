package web

import (
	"net/http"

	"github.com/kaecyra/gobbler/internal/web/layout"
)

// homeHandler is the placeholder landing page; the recipe list replaces it.
func homeHandler(w http.ResponseWriter, r *http.Request) {
	layout.Render(w, r, http.StatusOK, layout.Page("Home"))
}
