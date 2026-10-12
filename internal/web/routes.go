package web

import (
	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/store"
	webcatalog "github.com/kaecyra/gobbler/internal/web/catalog"
	"github.com/kaecyra/gobbler/internal/web/cook"
	"github.com/kaecyra/gobbler/internal/web/editor"
	"github.com/kaecyra/gobbler/internal/web/history"
	webingest "github.com/kaecyra/gobbler/internal/web/ingest"
	"github.com/kaecyra/gobbler/internal/web/journal"
	"github.com/kaecyra/gobbler/internal/web/recipeview"
	"github.com/kaecyra/gobbler/internal/web/settings"
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
	r.Mount(settings.Pattern, settings.Routes(settings.Deps{Backup: d.Backup, Secrets: d.BackupSecrets}))
	r.Mount(editor.Pattern, editor.Routes(editor.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB)}))
	mountCatalog(r, d)
}

// mountCatalog mounts the catalog cleanup page. Its catalog store is the one
// that merges ingredients, so the recipe store's merge hooks and usage counters
// are registered on it here: without the hooks a merge of an ingredient that
// recipes use fails on the foreign key, and without the counters the
// confirmation cannot state how many lines it re-points.
func mountCatalog(r chi.Router, d Deps) {
	cat, rec := catalog.New(d.DB), store.New(d.DB)
	usage := catalog.NewUsage(cat)
	rec.RegisterMergeHooks(cat)
	rec.RegisterUsageCounters(usage)
	r.Mount(webcatalog.Pattern, webcatalog.Routes(webcatalog.Deps{Catalog: cat, Usage: usage}))
}
