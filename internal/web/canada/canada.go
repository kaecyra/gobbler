package canada

import (
	_ "embed"
	"net/http"

	"github.com/a-h/templ"

	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Pattern is the page's path. The layout owns it, since the public-path list
// that lets anyone open it lives there (layout.IsPublic).
const Pattern = layout.CanadaPath

//go:embed canada.css
var stylesheet string

// styles is the page's stylesheet, written into the page like the history
// page's.
func styles() templ.Component {
	return templ.Raw("<style>" + stylesheet + "</style>")
}

// Handler serves the page.
func Handler(w http.ResponseWriter, r *http.Request) {
	layout.Render(w, r, http.StatusOK, page())
}
