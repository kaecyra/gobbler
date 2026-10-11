package web

import (
	"net/http"

	"github.com/kaecyra/gobbler/internal/web/layout"
)

// homeHandler is the placeholder landing page; the recipe list replaces it.
func homeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := layout.Page("Home").Render(r.Context(), w); err != nil {
		layout.WriteError(w, r, err)
	}
}
