// Package web is the HTTP server: the chi router, shared middleware, the
// health check and the embedded static assets. Feature pages live in
// subpackages under internal/web, each mounted from routes.go, and share the
// layout package (ADR-00012).
package web

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/ingest"
	"github.com/kaecyra/gobbler/internal/mcp"
	"github.com/kaecyra/gobbler/internal/oauth"
	"github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/ratelimit"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/store"
	"github.com/kaecyra/gobbler/internal/web/canada"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/nonmember"
	"github.com/kaecyra/gobbler/internal/web/settings"
	"github.com/kaecyra/gobbler/internal/web/signin"
)

// Identity establishes the identity of every request, refusing one without
// an identity unless public says its path is public. *auth.Authenticator
// satisfies it.
type Identity interface {
	Middleware(public func(path string) bool) func(http.Handler) http.Handler
}

// Deps is everything the server and the feature packages mounted on it need.
type Deps struct {
	Config config.Config
	DB     *sql.DB
	// Auth establishes identity on every request but the public paths
	// (layout.IsPublic, ADR-00029), where it is optional.
	Auth Identity
	// SignIn serves sign-in, its callback and sign-out (ADR-00030). It is
	// required when sign-in is configured (Config.OIDC.Configured) and must
	// be nil otherwise; with it the header offers "Sign in" and "Sign out".
	SignIn *signin.Service
	// Sessions has removing a member, or leaving, end that person's
	// sessions in the same transaction (ADR-00030). It is required exactly
	// when SignIn is set.
	Sessions *session.Store
	// Logger receives request logs; the default is slog.Default().
	Logger *slog.Logger
	// HealthDetails, when set, adds its entries to the health response under
	// "details". The health route is unauthenticated, so these must not carry
	// anything sensitive.
	HealthDetails func(ctx context.Context) map[string]string
	// Backup is the live backup job behind the settings page.
	Backup settings.Backup
	// BackupSecrets are the credential values redacted from the backup error
	// the settings page shows.
	BackupSecrets []string
	// Avatars runs avatar uploads (ADR-00033). It is required: the server
	// builds it over the configured media store and resumes interrupted jobs
	// before mounting the pages, and a nil one would mean a store chosen here,
	// not by the configuration.
	Avatars *profile.Avatars
	// MCPVerifier checks the bearer credential of the MCP endpoint (ADR-00034).
	// Nil means API keys (ADR-00031) and OAuth access tokens (ADR-00034).
	MCPVerifier mcp.Verifier
	// Drafts holds the drafts that wait on the review screen. Nil means the
	// proposals table in DB (ADR-00035).
	Drafts *ingest.Drafts
}

// New builds the router. Middleware order is fixed here: request logging, then
// panic recovery, then (with sign-in) the layout's sign-in marker, then
// authentication, then household membership
// (ADR-00018), then cross-origin protection (ADR-00016), then the handlers.
// Logging is outermost so refused requests (unauthenticated, not a member or
// cross-origin) and recovered panics (logged as 500) each get an access line,
// and the handlers never run for any kind of refusal.
func New(d Deps) (http.Handler, error) {
	return newRouter(d)
}

func newRouter(d Deps) (*chi.Mux, error) {
	if d.Auth == nil {
		return nil, errors.New("web: an authenticator is required")
	}
	if d.DB == nil {
		return nil, errors.New("web: a database is required")
	}
	if d.Avatars == nil {
		return nil, errors.New("web: the avatar service is required")
	}
	if d.Config.OIDC.Configured() != (d.SignIn != nil) {
		return nil, errors.New("web: the sign-in routes are required exactly when sign-in is configured")
	}
	if (d.SignIn != nil) != (d.Sessions != nil) {
		// Without the store, removing a member would leave their sessions
		// alive (ADR-00030).
		return nil, errors.New("web: the session store is required exactly when sign-in is mounted")
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}

	if d.MCPVerifier == nil {
		d.MCPVerifier = mcp.OAuthVerifier{Tokens: oauth.New(d.DB), Next: mcp.KeyVerifier{Keys: apikey.New(d.DB)}}
	}
	if d.Drafts == nil {
		d.Drafts = ingest.NewDrafts(store.New(d.DB))
	}

	var members memberLookup = household.New(d.DB)
	if d.Config.Server.DevAuthBypass {
		members = &devSeeded{store: household.New(d.DB)}
	}

	r := chi.NewRouter()
	// No trusted origins and no bypass patterns: no handler opts out (ADR-00016).
	cop := http.NewCrossOriginProtection()
	// First, so everything below, the session list and the OAuth limits
	// included, sees one client address (ADR-00036). The access log keeps the
	// connection's own address (remote_addr).
	r.Use(ratelimit.ResolveClient(d.Config.Server.TrustedProxies))
	r.Use(requestLogger(log), recoverer(log))
	if d.SignIn != nil {
		// Before identity, so the pages that refuse a request show it too.
		r.Use(markSignIn)
	}
	r.Use(onSession(d.Auth.Middleware(layout.IsPublic)), onBearer(siteOf(d.Config), mcp.Bearer(d.MCPVerifier, log)), membership(members, d.Config.Operator), cop.Handler)
	r.Use(avatarSource(d.DB, log))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		layout.WriteError(w, r, layout.ErrNotFound)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	})

	r.Get(HealthPath, healthHandler(d.HealthDetails))
	r.Mount(nonmember.Pattern, nonmember.Routes())
	r.Get(canada.Pattern, canada.Handler)
	if d.SignIn != nil {
		r.Get(layout.SignInPath, d.SignIn.SignIn)
		r.Get(layout.CallbackPath, d.SignIn.Callback)
		r.Post(layout.SignOutPath, d.SignIn.SignOut)
	}
	r.Handle("/static/*", staticHandler())
	mountFeatures(r, d)
	return r, nil
}

// markSignIn tells the layout that people sign in to this deployment, so the
// header offers sign-in and sign-out (layout.WithSignIn).
func markSignIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(layout.WithSignIn(r.Context())))
	})
}

// staticHandler serves the embedded assets under /static/, each with an ETag
// from its bytes (layout.SetAssetCache).
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("embedded static directory missing: " + err.Error())
	}
	etags := map[string]string{}
	err = fs.WalkDir(sub, ".", func(name string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		body, err := fs.ReadFile(sub, name)
		if err != nil {
			return err
		}
		etags[name] = layout.AssetETag(body)
		return nil
	})
	if err != nil {
		panic("embedded static directory unreadable: " + err.Error())
	}
	files := http.StripPrefix("/static/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// FileServerFS lists directories; only files are served. Clean the
		// path the way the file server does, or "/static/././" slips past.
		name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/static/")), "/")
		if name == "" {
			name = "."
		}
		if info, err := fs.Stat(sub, name); err == nil && info.IsDir() {
			layout.WriteError(w, r, layout.ErrNotFound)
			return
		}
		if etag, ok := etags[name]; ok {
			layout.SetAssetCache(w.Header(), etag)
		}
		files.ServeHTTP(w, r)
	})
}
