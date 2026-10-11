package web

import "github.com/go-chi/chi/v5"

// mountFeatures is where feature packages attach their routes (ADR-00012). A
// feature exposes a constructor that returns its routes; this node imports
// none. Append one line per feature below, for example:
//
//	r.Mount("/recipes", recipes.Routes(recipes.Deps{DB: d.DB}))
//
// The router has already applied authentication by the time r is passed here.
func mountFeatures(r chi.Router, d Deps) {
	_, _ = r, d
}
