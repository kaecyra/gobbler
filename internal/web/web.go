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

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/web/layout"
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
}

// New builds the router. Middleware order is fixed here: panic recovery, then
// request logging (so refused requests are logged), then authentication, then
// the handlers, which therefore never run for an unauthenticated request.
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
	r.Use(recoverer(log), requestLogger(log), d.Auth.Middleware(HealthPath))
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
		w.Header().Set("Cache-Control", "public, max-age=3600")
		files.ServeHTTP(w, r)
	})
}
