package web

import (
	"github.com/go-chi/chi/v5"

	webingest "github.com/kaecyra/gobbler/internal/web/ingest"
)

// mountFeatures is where feature packages attach their routes (ADR-00012). A
// feature exposes a constructor that returns its routes. Append one line per
// feature below, for example:
//
//	r.Mount("/recipes", recipes.Routes(recipes.Deps{DB: d.DB}))
//
// The router has already applied authentication by the time r is passed here.
func mountFeatures(r chi.Router, d Deps) {
	r.Mount("/ingest", webingest.Routes(webingest.Deps{DB: d.DB, Ingestion: d.Config.Ingestion, Logger: d.Logger}))
}
