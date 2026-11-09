package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/api/caller"
	apicatalog "github.com/kaecyra/gobbler/internal/api/catalog"
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	apijournal "github.com/kaecyra/gobbler/internal/api/journal"
	apiprofile "github.com/kaecyra/gobbler/internal/api/profile"
	apirecipes "github.com/kaecyra/gobbler/internal/api/recipes"
	apisharing "github.com/kaecyra/gobbler/internal/api/sharing"
	apishop "github.com/kaecyra/gobbler/internal/api/shopping"
	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/mcp"
	"github.com/kaecyra/gobbler/internal/oauth"
	"github.com/kaecyra/gobbler/internal/ratelimit"
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

// admitOnce lets the catalog service check a request's Caller once: the line
// builder reads the catalog once per ingredient line, and each read would
// otherwise look the person's household up again (apicatalog.WithAdmission).
func admitOnce(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(apicatalog.WithAdmission(r.Context())))
	})
}

// mountFeatures is where feature packages attach their routes (ADR-00012). A
// feature exposes a constructor that returns its routes. Append one line per
// feature below, for example:
//
//	r.Mount("/recipes", recipes.Routes(recipes.Deps{DB: d.DB}))
//
// The router has already applied authentication by the time r is passed here.
func mountFeatures(r chi.Router, d Deps) {
	r.Mount("/ingest", webingest.Routes(webingest.Deps{DB: d.DB, Recipes: d.Recipes, Catalog: d.Catalog, Ingestion: d.Config.Ingestion, LLM: d.Config.LLM, Logger: d.Logger}))
	r.Mount(recipeview.Pattern, recipeview.Routes(recipeview.Deps{Recipes: d.Recipes, Catalog: d.Catalog, Pictures: recipePictures(d)}))
	r.Mount(recipeview.DeletePattern, recipeview.DeleteRoutes(recipeview.DeleteDeps{Recipes: d.Recipes}))
	mountJournal(r, d)
	r.Mount(cook.Pattern, cook.Routes(cook.Deps{Recipes: d.Recipes, Catalog: d.Catalog}))
	r.Mount(history.Pattern, history.Routes(history.Deps{Recipes: d.Recipes, Catalog: d.Catalog}))
	r.Mount(history.ClonePattern, history.CloneRoutes(history.Deps{Recipes: d.Recipes, Catalog: d.Catalog}))
	r.Mount(settings.Pattern, settings.Routes(settings.Deps{Backup: d.Backup, Secrets: d.BackupSecrets}))
	// The deployment page moved under the operator area; old bookmarks follow.
	r.Get(settings.LegacyPattern, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, settings.Pattern, http.StatusMovedPermanently)
	})
	r.Mount(editor.Pattern, editor.Routes(editor.Deps{Recipes: d.Recipes, Catalog: d.Catalog, Pictures: d.Recipes}))
	r.Mount(recipes.Pattern, recipes.Routes(recipes.Deps{Library: d.Recipes, Pictures: recipePictures(d)}))
	// The home page: "/" is registered here and nowhere else.
	r.Get("/", homeRoute(d))
	mountCatalog(r, d)
	mountHouseholds(r, d)
	mountPromotion(r, d)
	mountSharing(r, d)
	mountCentralCatalog(r, d)
	mountProfile(r, d)
	mountMCP(r, d)
	mountOAuth(r, d)
	mountPictures(r, d)
}

// mountJournal mounts the recipe journal over the journal service. A service
// that cannot be built leaves the route answering 500 with the cause, as the
// shopping pages do, so a bad build is a loud failure and not a missing page.
func mountJournal(r chi.Router, d Deps) {
	svc, err := apijournal.New(apijournal.Deps{Recipes: apirecipes.NewStore(d.DB), Members: apihouseholds.NewStore(d.DB)})
	if err != nil {
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("journal unavailable", "error", err.Error())
		r.Mount(journal.Pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the journal is not available: "+err.Error(), http.StatusInternalServerError)
		}))
		return
	}
	r.Mount(journal.Pattern, journal.Routes(journal.Deps{Journal: svc}))
}

// mountCatalogPage mounts the catalog cleanup page over the catalog service
// (ADR-00043). cat is the merging store and usage its counter, so the page
// merges and counts through the service.
func mountCatalogPage(r chi.Router, d Deps, cat *apicatalog.Store, usage *apicatalog.Usage) {
	svc, err := apicatalog.New(apicatalog.Deps{Catalog: cat, Members: apihouseholds.NewStore(d.DB), Usage: usage})
	if err != nil {
		// Unreachable while the deps above are non-nil; fail loud if that changes.
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("catalog page unavailable", "error", err.Error())
		r.Mount(webcatalog.Pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the catalog page is not available: "+err.Error(), http.StatusInternalServerError)
		}))
		return
	}
	bound := svc.Bind(caller.FromContext)
	r.Mount(webcatalog.Pattern, webcatalog.Routes(webcatalog.Deps{Catalog: bound, Usage: bound, Recipes: d.Recipes}))
}

// mountCatalog mounts the catalog cleanup page and the shopping pages. Its
// catalog store is the one that merges ingredients, so the recipe store's merge
// hooks and usage counters are registered on it here: without the hooks a merge
// of an ingredient that recipes use fails on the foreign key, and without the
// counters the confirmation cannot state how many rows it re-points.
func mountCatalog(r chi.Router, d Deps) {
	cat, rec := apicatalog.NewStore(d.DB), apirecipes.NewStore(d.DB)
	usage := apicatalog.NewUsage(cat)
	rec.RegisterMergeHooks(cat)
	rec.RegisterUsageCounters(usage)
	mountCatalogPage(r, d, cat, usage)

	// The shopping pages share the merging catalog store: the shopping store's
	// hooks re-point its manual items, pantry and checks on a merge, and its
	// counter adds those rows to the merge confirmation.
	shop := apishop.NewStore(d.DB, rec, cat)
	shop.RegisterMergeHooks(cat)
	shop.RegisterUsageCounters(usage)
	svc, err := apishop.New(apishop.Deps{Lists: shop, Recipes: rec, Catalog: cat, Members: apihouseholds.NewStore(d.DB)})
	if err != nil {
		// Every route still exists and answers 500 with the cause, as the MCP
		// endpoint does, so a bad build is a loud failure and not a missing page.
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("shopping pages unavailable", "error", err.Error())
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
	hs := apihouseholds.NewStore(d.DB)
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
	// Deleting a household also deletes its picture objects (ADR-00039), which
	// the store the service calls does through pictures.Households.
	svc, err := apihouseholds.New(apihouseholds.Deps{
		Households: pictures.Households{Store: hs, Reaper: apirecipes.NewReaper(d.Media, d.Logger)},
		Operators:  d.Config.Operator,
	})
	if err != nil {
		// Unreachable while the deps above are non-nil; fail loud if that changes.
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("household pages unavailable", "error", err.Error())
		unavailable := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the household pages are not available: "+err.Error(), http.StatusInternalServerError)
		})
		r.Mount(webhousehold.Pattern, unavailable)
		r.Mount(webhousehold.InvitationsPattern, unavailable)
		r.Mount(operator.Pattern, unavailable)
		return
	}
	r.Mount(webhousehold.Pattern, webhousehold.MemberRoutes(webhousehold.Deps{Households: svc, Keys: keys}))
	r.Mount(webhousehold.InvitationsPattern, webhousehold.InvitationRoutes(webhousehold.Deps{Households: svc}))
	mountOperator(r, d, svc)
}

// mountOperator mounts the operator area's overview and invitation pages
// (ADR-00018). The overview reads the pending invitations, the suggested
// ingredients and the backup status.
func mountOperator(r chi.Router, d Deps, households operator.Households) {
	cat, err := apicatalog.New(apicatalog.Deps{Catalog: apicatalog.NewStore(d.DB), Members: apihouseholds.NewStore(d.DB), Operators: d.Config.Operator})
	if err == nil && d.Backup == nil {
		err = errors.New("the backup job is required")
	}
	if err != nil {
		// Production always passes the backup job (cmd/gobbler); a router
		// built without one serves this error rather than a nil dereference.
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("operator pages unavailable", "error", err.Error())
		r.Mount(operator.Pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the operator pages are not available: "+err.Error(), http.StatusInternalServerError)
		}))
		return
	}
	r.Mount(operator.Pattern, operator.Routes(operator.Deps{Households: households, Suggestions: cat.Bind(caller.FromContext), Backup: d.Backup}))
}

// mountPromotion mounts the operator's review of overlay entries suggested for
// the central catalog (ADR-00018). Its catalog store has the merge hooks of the
// stores that hold ingredient references, because promoting merges other
// households' duplicates into the promoted entry and re-points their rows.
func mountPromotion(r chi.Router, d Deps) {
	cat := apicatalog.NewStore(d.DB)
	apirecipes.NewStore(d.DB).RegisterMergeHooks(cat)
	apishop.NewStore(d.DB, apirecipes.NewStore(d.DB), cat).RegisterMergeHooks(cat)
	svc, err := apicatalog.New(apicatalog.Deps{Catalog: cat, Members: apihouseholds.NewStore(d.DB), Operators: d.Config.Operator})
	if err != nil {
		// Unreachable while the deps above are non-nil; fail loud if that changes.
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("promotion pages unavailable", "error", err.Error())
		r.Mount(operator.PromotionPattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the promotion pages are not available: "+err.Error(), http.StatusInternalServerError)
		}))
		return
	}
	r.Mount(operator.PromotionPattern, operator.PromotionRoutes(operator.PromotionDeps{Catalog: svc.Bind(caller.FromContext)}))
}

// mountSharing mounts the pages that send a recipe copy to another household
// and accept or decline one sent to this household (ADR-00018, Sharing).
func mountSharing(r chi.Router, d Deps) {
	hs := apihouseholds.NewStore(d.DB)
	svc, err := apisharing.New(apisharing.Deps{Recipes: apirecipes.NewStore(d.DB), Households: hs, Catalog: apicatalog.NewStore(d.DB), Members: hs})
	if err != nil {
		// Unreachable while the deps above are non-nil; fail loud if that changes.
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("sharing pages unavailable", "error", err.Error())
		r.Mount(websharing.Pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the sharing pages are not available: "+err.Error(), http.StatusInternalServerError)
		}))
		return
	}
	r.Mount(websharing.Pattern, websharing.Routes(websharing.Deps{Shares: svc, Recipes: d.Recipes}))
}

// mountCentralCatalog mounts the operator's curation of the central catalog:
// the equipment review, where the sized flag behind the scaled view's
// larger-pan hint is set (ADR-00019), and the central ingredient editor
// (ADR-00018, Catalog). Both sit under /operator, so the membership middleware
// admits only an operator.
func mountCentralCatalog(r chi.Router, d Deps) {
	svc, err := apicatalog.New(apicatalog.Deps{Catalog: apicatalog.NewStore(d.DB), Members: apihouseholds.NewStore(d.DB), Operators: d.Config.Operator})
	if err != nil {
		// Unreachable while the deps above are non-nil; fail loud if that changes.
		log := d.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Error("central catalog pages unavailable", "error", err.Error())
		for _, pattern := range []string{webcatalog.EquipmentPattern, webcatalog.CentralPattern} {
			r.Mount(pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "the central catalog pages are not available: "+err.Error(), http.StatusInternalServerError)
			}))
		}
		return
	}
	bound := svc.Bind(caller.FromContext)
	r.Mount(webcatalog.EquipmentPattern, webcatalog.EquipmentRoutes(webcatalog.EquipmentDeps{Catalog: bound}))
	r.Mount(webcatalog.CentralPattern, webcatalog.CentralRoutes(webcatalog.CentralDeps{Catalog: bound}))
}

// newProfileService builds the profile service over the database, the avatar
// service and, when sign-in is mounted, the session store.
func newProfileService(d Deps) (*apiprofile.Service, error) {
	deps := apiprofile.Deps{
		Profiles: apiprofile.NewStore(d.DB), Households: apihouseholds.NewStore(d.DB),
		Keys: apikey.New(d.DB), Grants: oauth.New(d.DB), Avatars: d.Avatars,
	}
	// A nil *session.Store in the interface would not be nil: sessions exist
	// only when sign-in is mounted.
	if d.Sessions != nil {
		deps.Sessions = d.Sessions
	}
	return apiprofile.New(deps)
}

// mountProfile mounts the profile pages (ADR-00032). A verified non-member may
// open them, since every signed-in person has a profile (layout.Admits). The
// profile service is the one place the page reads and writes the profile, the
// person's sessions, keys and connected apps, and their avatar (ADR-00043).
func mountProfile(r chi.Router, d Deps) {
	r.Mount(webprofile.Pattern, webprofile.Routes(webprofile.Deps{Profile: d.Profile}))
}

// mountMCP mounts the MCP endpoint (ADR-00034). By the time a request reaches
// it, newRouter has authenticated its bearer credential and applied the
// membership check, so its tools run in the credential's household. Its stores
// share the merging catalog's hooks like the shopping pages do.
func mountMCP(r chi.Router, d Deps) {
	// propose_recipe_edit supersedes a pending re-import (ADR-00042); the recipe
	// service d.Recipes does it with a store that deletes the re-import's
	// picture candidates. rec is the store the shopping service reads recipes
	// from and the merge hooks and usage counters attach to.
	cat, rec := apicatalog.NewStore(d.DB), apirecipes.NewStore(d.DB)
	usage := apicatalog.NewUsage(cat)
	rec.RegisterMergeHooks(cat)
	rec.RegisterUsageCounters(usage)
	shop := apishop.NewStore(d.DB, rec, cat)
	shop.RegisterMergeHooks(cat)
	shop.RegisterUsageCounters(usage)
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	deps, err := mcp.NewDeps(d.Recipes, rec, shop, apihouseholds.NewStore(d.DB), cat, mcp.AuditByKind{mcp.KindAPIKey: mcp.KeyAudit{Keys: apikey.New(d.DB)}, mcp.KindOAuth: mcp.GrantAudit{Grants: oauth.New(d.DB)}}, d.Config, log)
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
	deps := pictures.Deps{Pictures: d.Recipes, Media: d.Media, Logger: d.Logger}
	r.Mount(pictures.Pattern, pictures.Routes(deps))
	r.Mount(pictures.DraftsPattern, pictures.DraftRoutes(deps))
}

// sessionPictureKeys is the picture key lookup the recipe screens' Pictures
// take, over the recipe service: it asks as the caller of the request the
// context carries, and the service refuses one that is not a member.
type sessionPictureKeys struct{ svc *apirecipes.Service }

func (k sessionPictureKeys) Key(ctx context.Context, id int64) (string, bool, error) {
	return k.svc.PictureKey(ctx, caller.FromContext(ctx), id)
}

// recipePictures gives the library and the recipe page the address of a
// recipe's picture.
func recipePictures(d Deps) pictures.Srcs {
	return pictures.Srcs{Pictures: sessionPictureKeys{svc: d.Recipes}}
}
