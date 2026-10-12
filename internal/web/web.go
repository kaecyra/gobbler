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

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/settings"
)

// Deps is everything the server and the feature packages mounted on it need.
type Deps struct {
	Config config.Config
	DB     *sql.DB
	// Auth verifies the Access JWT on every request but the health check.
	Auth *auth.Authenticator
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
}

// New builds the router. Middleware order is fixed here: request logging, then
// panic recovery, then authentication, then cross-origin protection
// (ADR-00016), then the handlers. Logging is outermost so refused requests
// (unauthenticated or cross-origin) and recovered panics (logged as 500) each
// get an access line, and the handlers never run for either kind of refusal.
func New(d Deps) (http.Handler, error) {
	return newRouter(d)
}

func newRouter(d Deps) (*chi.Mux, error) {
	if d.Auth == nil {
		return nil, errors.New("web: an authenticator is required")
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}

	r := chi.NewRouter()
	// No trusted origins and no bypass patterns: no handler opts out (ADR-00016).
	cop := http.NewCrossOriginProtection()
	r.Use(requestLogger(log), recoverer(log), d.Auth.Middleware(HealthPath), cop.Handler)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		layout.WriteError(w, r, layout.ErrNotFound)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	})

	r.Get(HealthPath, healthHandler(d.HealthDetails))
	r.Get("/", homeHandler)
	r.Handle("/static/*", staticHandler())
	mountFeatures(r, d)
	return r, nil
}

// staticHandler serves the embedded assets under /static/.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("embedded static directory missing: " + err.Error())
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
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}
