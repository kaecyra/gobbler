package web

import (
	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/cook"
	"github.com/kaecyra/gobbler/internal/web/history"
	webingest "github.com/kaecyra/gobbler/internal/web/ingest"
	"github.com/kaecyra/gobbler/internal/web/journal"
	"github.com/kaecyra/gobbler/internal/web/recipeview"
)

// mountFeatures is where feature packages attach their routes (ADR-00012). A
// feature exposes a constructor that returns its routes. Append one line per
// feature below, for example:
//
//	r.Mount("/recipes", recipes.Routes(recipes.Deps{DB: d.DB}))
//
// The router has already applied authentication by the time r is passed here.
func mountFeatures(r chi.Router, d Deps) {
	r.Mount("/ingest", webingest.Routes(webingest.Deps{DB: d.DB, Ingestion: d.Config.Ingestion, LLM: d.Config.LLM, Logger: d.Logger}))
	r.Mount(recipeview.Pattern, recipeview.Routes(recipeview.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB)}))
	r.Mount(journal.Pattern, journal.Routes(journal.Deps{Journal: store.New(d.DB)}))
	r.Mount(cook.Pattern, cook.Routes(cook.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB)}))
	r.Mount(history.Pattern, history.Routes(history.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB)}))
	r.Mount(history.ClonePattern, history.CloneRoutes(history.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB)}))
}
