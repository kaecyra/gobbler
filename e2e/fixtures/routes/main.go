// Command routes prints every route the real internal/web router registers, as
// JSON, for the e2e route-coverage check (ADR-00013 obligation 2). It builds
// the router the way `gobbler serve` does, with the dev auth bypass and a
// throwaway database, then walks it with chi, so mounted feature routes appear
// with their full paths. Sign-in is configured too (ADR-00030), as it is in
// production, so its routes are listed; its issuer is never contacted, since
// discovery waits for the first sign-in.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/web"
	"github.com/kaecyra/gobbler/internal/web/signin"
)

type route struct {
	Method  string `json:"method"`
	Pattern string `json:"pattern"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "routes:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "gobbler-routes-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(os.Stderr, "routes: remove temp dir:", err)
		}
	}()

	cfg := config.Config{
		Server: config.Server{
			ListenAddr:    "127.0.0.1:0",
			DataDir:       dir,
			DevAuthBypass: true,
		},
		OIDC:    config.OIDC{Issuer: "http://127.0.0.1:1", ClientID: "routes", PublicBaseURL: "http://127.0.0.1:0"},
		Session: config.Session{Lifetime: time.Hour},
		// The ingest routes and the MCP endpoint refuse to mount without these,
		// and would then be walked as one catch-all each, hiding their routes
		// from the coverage check.
		Ingestion: config.Ingestion{FetchTimeout: 15 * time.Second, UserAgent: "gobbler-routes", ConfidenceThreshold: 0.5},
		MCP:       config.MCP{Rate: 1, Burst: 1},
	}
	d, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := d.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "routes: close database:", err)
		}
	}()
	authn, err := auth.New(cfg, auth.WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		return err
	}
	sessions, err := session.New(d, cfg.Session.Lifetime)
	if err != nil {
		return err
	}
	signIn, err := signin.New(signin.Settings{
		Issuer: cfg.OIDC.Issuer, ClientID: cfg.OIDC.ClientID, PublicBaseURL: cfg.OIDC.PublicBaseURL, Lifetime: cfg.Session.Lifetime,
	}, sessions)
	if err != nil {
		return err
	}
	h, err := web.New(web.Deps{Config: cfg, DB: d, Auth: authn, SignIn: signIn, Sessions: sessions,
		Avatars: profile.NewAvatars(ctx, profile.New(d), media.NewLocal(filepath.Join(dir, media.LocalDirName)), nil)})
	if err != nil {
		return err
	}
	r, ok := h.(chi.Routes)
	if !ok {
		return fmt.Errorf("web.New returned %T, which is not a chi router", h)
	}

	routes := []route{}
	err = chi.Walk(r, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, route{Method: method, Pattern: pattern})
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk routes: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(routes)
}
