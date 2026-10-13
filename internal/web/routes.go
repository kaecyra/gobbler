package web

import (
	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/sharing"
	"github.com/kaecyra/gobbler/internal/shopping"
	"github.com/kaecyra/gobbler/internal/store"
	webcatalog "github.com/kaecyra/gobbler/internal/web/catalog"
	"github.com/kaecyra/gobbler/internal/web/cook"
	"github.com/kaecyra/gobbler/internal/web/editor"
	"github.com/kaecyra/gobbler/internal/web/history"
	webhousehold "github.com/kaecyra/gobbler/internal/web/household"
	webingest "github.com/kaecyra/gobbler/internal/web/ingest"
	"github.com/kaecyra/gobbler/internal/web/journal"
	"github.com/kaecyra/gobbler/internal/web/operator"
	"github.com/kaecyra/gobbler/internal/web/recipes"
	"github.com/kaecyra/gobbler/internal/web/recipeview"
	"github.com/kaecyra/gobbler/internal/web/settings"
	websharing "github.com/kaecyra/gobbler/internal/web/sharing"
	webshopping "github.com/kaecyra/gobbler/internal/web/shopping"
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
	r.Mount(recipes.Pattern, recipes.Routes(recipes.Deps{Library: store.New(d.DB)}))
	mountCatalog(r, d)
	mountHouseholds(r, d)
	mountPromotion(r, d)
	mountSharing(r, d)
	mountEquipment(r, d)
}

// mountCatalog mounts the catalog cleanup page and the shopping pages. Its
// catalog store is the one that merges ingredients, so the recipe store's merge
// hooks and usage counters are registered on it here: without the hooks a merge
// of an ingredient that recipes use fails on the foreign key, and without the
// counters the confirmation cannot state how many rows it re-points.
func mountCatalog(r chi.Router, d Deps) {
	cat, rec := catalog.New(d.DB), store.New(d.DB)
	usage := catalog.NewUsage(cat)
	rec.RegisterMergeHooks(cat)
	rec.RegisterUsageCounters(usage)
	r.Mount(webcatalog.Pattern, webcatalog.Routes(webcatalog.Deps{Catalog: cat, Usage: usage}))

	// The shopping pages share the merging catalog store: the shopping store's
	// hooks re-point its manual items, pantry and checks on a merge, and its
	// counter adds those rows to the merge confirmation.
	shop := shopping.New(d.DB, rec, cat)
	shop.RegisterMergeHooks(cat)
	shop.RegisterUsageCounters(usage)
	r.Mount(webshopping.Pattern, webshopping.Routes(webshopping.Deps{Lists: shop, Recipes: rec, Catalog: cat, Logger: d.Logger}))
}

// mountHouseholds mounts the members page, invitation acceptance (reachable by
// a verified non-member) and the operator's invitation page (ADR-00018).
func mountHouseholds(r chi.Router, d Deps) {
	hs := household.New(d.DB)
	r.Mount(webhousehold.Pattern, webhousehold.MemberRoutes(webhousehold.Deps{Households: hs}))
	r.Mount(webhousehold.InvitationsPattern, webhousehold.InvitationRoutes(webhousehold.Deps{Households: hs}))
	r.Mount(operator.Pattern, operator.Routes(operator.Deps{Households: hs}))
}

// mountPromotion mounts the operator's review of overlay entries suggested for
// the central catalog (ADR-00018). Its catalog store has the merge hooks of the
// stores that hold ingredient references, because promoting merges other
// households' duplicates into the promoted entry and re-points their rows.
func mountPromotion(r chi.Router, d Deps) {
	cat := catalog.New(d.DB)
	store.New(d.DB).RegisterMergeHooks(cat)
	shopping.New(d.DB, store.New(d.DB), cat).RegisterMergeHooks(cat)
	r.Mount(operator.PromotionPattern, operator.PromotionRoutes(operator.PromotionDeps{Catalog: cat}))
}

// mountSharing mounts the pages that send a recipe copy to another household
// and accept or decline one sent to this household (ADR-00018, Sharing).
func mountSharing(r chi.Router, d Deps) {
	rec := store.New(d.DB)
	svc := sharing.New(rec, household.New(d.DB), catalog.New(d.DB))
	r.Mount(websharing.Pattern, websharing.Routes(websharing.Deps{Shares: svc, Recipes: rec}))
}

// mountEquipment mounts the operator's review of catalog equipment, where the
// sized flag behind the scaled view's larger-pan hint is set (ADR-00019). It
// sits under /operator, so the membership middleware admits only an operator.
func mountEquipment(r chi.Router, d Deps) {
	r.Mount(webcatalog.EquipmentPattern, webcatalog.EquipmentRoutes(webcatalog.EquipmentDeps{Catalog: catalog.New(d.DB)}))
}
