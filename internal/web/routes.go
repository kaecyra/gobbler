package web

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/catalog"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/mcp"
	"github.com/kaecyra/gobbler/internal/oauth"
	domain "github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/ratelimit"
	"github.com/kaecyra/gobbler/internal/sharing"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/video"
	webcatalog "github.com/kaecyra/gobbler/internal/web/catalog"
	"github.com/kaecyra/gobbler/internal/web/cook"
	"github.com/kaecyra/gobbler/internal/web/editor"
	"github.com/kaecyra/gobbler/internal/web/history"
	webhousehold "github.com/kaecyra/gobbler/internal/web/household"
	webingest "github.com/kaecyra/gobbler/internal/web/ingest"
	"github.com/kaecyra/gobbler/internal/web/journal"
	weboauth "github.com/kaecyra/gobbler/internal/web/oauth"
	"github.com/kaecyra/gobbler/internal/web/operator"
	pictures "github.com/kaecyra/gobbler/internal/web/picture"
	webprofile "github.com/kaecyra/gobbler/internal/web/profile"
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
	// One fetcher, built from configuration, serves every save path that
	// downloads a video thumbnail, so the image host is set in one place.
	thumbs := video.NewFetcherAt(d.Config.Video.ThumbnailHost)
	r.Mount("/ingest", webingest.Routes(webingest.Deps{DB: d.DB, Thumbnails: thumbs, Pending: d.Drafts, Pictures: d.Pictures, Ingestion: d.Config.Ingestion, LLM: d.Config.LLM, Logger: d.Logger}))
	r.Mount(recipeview.Pattern, recipeview.Routes(recipeview.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB), Pictures: pictures.Srcs{Pictures: d.Pictures}}))
	r.Mount(recipeview.DeletePattern, recipeview.DeleteRoutes(recipeview.DeleteDeps{Recipes: store.New(d.DB), Reaper: domain.NewReaper(d.Media, d.Logger)}))
	r.Mount(journal.Pattern, journal.Routes(journal.Deps{Journal: store.New(d.DB)}))
	r.Mount(cook.Pattern, cook.Routes(cook.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB)}))
	r.Mount(history.Pattern, history.Routes(history.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB), Thumbnails: thumbs}))
	r.Mount(history.ClonePattern, history.CloneRoutes(history.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB), Thumbnails: thumbs}))
	r.Mount(settings.Pattern, settings.Routes(settings.Deps{Backup: d.Backup, Secrets: d.BackupSecrets}))
	r.Mount(editor.Pattern, editor.Routes(editor.Deps{Recipes: store.New(d.DB), Catalog: catalog.New(d.DB), Thumbnails: thumbs}))
	r.Mount(recipes.Pattern, recipes.Routes(recipes.Deps{Library: store.New(d.DB), Pictures: pictures.Srcs{Pictures: d.Pictures}}))
	// The home page: "/" is registered here and nowhere else.
	r.Get("/", homeRoute(d))
	mountCatalog(r, d)
	mountHouseholds(r, d)
	mountPromotion(r, d)
	mountSharing(r, d, thumbs)
	mountEquipment(r, d)
	mountProfile(r, d)
	mountMCP(r, d)
	mountOAuth(r, d)
	mountPictures(r, d)
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
	shop := apishop.NewStore(d.DB, rec, cat)
	shop.RegisterMergeHooks(cat)
	shop.RegisterUsageCounters(usage)
	svc, err := apishop.New(apishop.Deps{Lists: shop, Recipes: rec, Catalog: cat, Members: household.New(d.DB)})
	if err != nil {
		// Every route still exists and answers 500 with the cause, as the MCP
		// endpoint does, so a bad build is a loud failure and not a missing page.
		slog.Error("shopping pages unavailable", "error", err.Error())
		r.Mount(webshopping.Pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the shopping pages are not available: "+err.Error(), http.StatusInternalServerError)
		}))
		return
	}
	r.Mount(webshopping.Pattern, webshopping.Routes(webshopping.Deps{Shop: svc, Logger: d.Logger}))
}

// mountHouseholds mounts the members page, invitation acceptance (reachable by
// a verified non-member) and the operator's invitation page (ADR-00018).
//
// It is the only household store that removes members, so it is the one that
// ends a former member's sessions (ADR-00030).
func mountHouseholds(r chi.Router, d Deps) {
	hs := household.New(d.DB)
	// Nil exactly when sign-in is not mounted (newRouter checks), and then
	// there are no sessions to end.
	if d.Sessions != nil {
		hs.OnMemberLeave(d.Sessions.EndForEmail)
	}
	// Removing a member, or their leaving, revokes their API keys in the same
	// transaction (ADR-00031).
	keys := apikey.New(d.DB)
	hs.OnMemberLeave(keys.RevokeForMember)
	// And their OAuth grants (ADR-00034), in that same transaction.
	hs.OnMemberLeave(oauth.New(d.DB).RevokeForMember)
	r.Mount(webhousehold.Pattern, webhousehold.MemberRoutes(webhousehold.Deps{Households: pictures.Households{Store: hs, Reaper: domain.NewReaper(d.Media, d.Logger)}, Keys: keys}))
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
	apishop.NewStore(d.DB, store.New(d.DB), cat).RegisterMergeHooks(cat)
	r.Mount(operator.PromotionPattern, operator.PromotionRoutes(operator.PromotionDeps{Catalog: cat}))
}

// mountSharing mounts the pages that send a recipe copy to another household
// and accept or decline one sent to this household (ADR-00018, Sharing).
func mountSharing(r chi.Router, d Deps, thumbs video.ThumbnailSource) {
	rec := store.New(d.DB)
	svc := sharing.New(rec, household.New(d.DB), catalog.New(d.DB))
	r.Mount(websharing.Pattern, websharing.Routes(websharing.Deps{Shares: svc, Recipes: rec, Thumbnails: thumbs}))
}

// mountEquipment mounts the operator's review of catalog equipment, where the
// sized flag behind the scaled view's larger-pan hint is set (ADR-00019). It
// sits under /operator, so the membership middleware admits only an operator.
func mountEquipment(r chi.Router, d Deps) {
	r.Mount(webcatalog.EquipmentPattern, webcatalog.EquipmentRoutes(webcatalog.EquipmentDeps{Catalog: catalog.New(d.DB)}))
}

// mountProfile mounts the profile pages (ADR-00032). A verified non-member may
// open them, since every signed-in person has a profile (layout.Admits). The
// sections later nodes add to the page take what they need from these deps.
func mountProfile(r chi.Router, d Deps) {
	r.Mount(webprofile.Pattern, webprofile.Routes(webprofile.Deps{
		Profiles: profile.New(d.DB), Households: household.New(d.DB),
		DB: d.DB, Config: d.Config, Sessions: d.Sessions, Avatars: d.Avatars,
	}))
}

// mountMCP mounts the MCP endpoint (ADR-00034). By the time a request reaches
// it, newRouter has authenticated its bearer credential and applied the
// membership check, so its tools run in the credential's household. Its stores
// share the merging catalog's hooks like the shopping pages do.
func mountMCP(r chi.Router, d Deps) {
	cat, rec := catalog.New(d.DB), store.New(d.DB)
	usage := catalog.NewUsage(cat)
	rec.RegisterMergeHooks(cat)
	rec.RegisterUsageCounters(usage)
	shop := apishop.NewStore(d.DB, rec, cat)
	shop.RegisterMergeHooks(cat)
	shop.RegisterUsageCounters(usage)
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	deps, err := mcp.NewDeps(rec, shop, household.New(d.DB), cat, d.Drafts, mcp.AuditByKind{mcp.KindAPIKey: mcp.KeyAudit{Keys: apikey.New(d.DB)}, mcp.KindOAuth: mcp.GrantAudit{Grants: oauth.New(d.DB)}}, d.Config, log)
	if err == nil {
		var h http.Handler
		if h, err = mcp.Routes(deps); err == nil {
			r.Mount(mcp.Pattern, h)
			return
		}
	}
	// Every route still exists and answers 500 with the cause, as the ingest
	// routes do, so a bad configuration is a loud failure and not a missing page.
	log.Error("mcp endpoint unavailable", "error", err.Error())
	r.Mount(mcp.Pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the MCP endpoint is not available: "+err.Error(), http.StatusInternalServerError)
	}))
}

// mountOAuth mounts the OAuth authorization server for MCP clients (ADR-00034):
// the discovery documents, client registration and the token endpoint are
// public (layout.IsPublic), and the authorization endpoint needs a session.
func mountOAuth(r chi.Router, d Deps) {
	deps := weboauth.Deps{
		OAuth: oauth.New(d.DB), Metadata: oauth.NewMetadataFetcher(),
		PublicBaseURL: d.Config.OIDC.PublicBaseURL, DevBypass: d.Config.Server.DevAuthBypass, Logger: d.Logger,
		Register: ratelimit.New(d.Config.OAuth.Register.Rate, d.Config.OAuth.Register.Burst, time.Now),
		Token:    ratelimit.New(d.Config.OAuth.Token.Rate, d.Config.OAuth.Token.Burst, time.Now),
	}
	r.Mount(weboauth.WellKnownPattern, weboauth.WellKnownRoutes(deps))
	routes, err := weboauth.Routes(deps)
	if err != nil {
		// A router that cannot be built still answers 500 with the cause, as
		// the MCP mount does, so a wiring fault is loud and not a missing page.
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("oauth endpoints unavailable", "error", err.Error())
		routes = chi.NewRouter()
		routes.Handle("/*", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the OAuth endpoints are not available: "+err.Error(), http.StatusInternalServerError)
		}))
	}
	r.Mount(weboauth.Pattern, routes)
}

// mountPictures mounts the routes that serve a recipe's picture and a draft's
// candidates from the media store, after admission (ADR-00039).
func mountPictures(r chi.Router, d Deps) {
	deps := pictures.Deps{Pictures: d.Pictures, Media: d.Media, Logger: d.Logger}
	r.Mount(pictures.Pattern, pictures.Routes(deps))
	r.Mount(pictures.DraftsPattern, pictures.DraftRoutes(deps))
}
