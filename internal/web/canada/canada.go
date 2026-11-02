package canada

import (
	"net/http"

	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is the page's path. The layout owns it, since the public-path list
// that lets anyone open it lives there (layout.IsPublic).
const Pattern = layout.CanadaPath

// Handler serves the page.
func Handler(w http.ResponseWriter, r *http.Request) {
	layout.Render(w, r, http.StatusOK, page())
}
