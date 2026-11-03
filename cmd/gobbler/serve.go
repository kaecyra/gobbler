package main

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"

	"github.com/kaecyra/gobbler/internal/auth"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/session"
	"github.com/kaecyra/gobbler/internal/web"
	"github.com/kaecyra/gobbler/internal/web/layout"
	"github.com/kaecyra/gobbler/internal/web/settings"
	"github.com/kaecyra/gobbler/internal/web/signin"
)

// listenFunc opens the listener for addr; a parameter so tests can observe
// whether serve got as far as listening.
type listenFunc func(ctx context.Context, addr string) (net.Listener, error)

func tcpListen(ctx context.Context, addr string) (net.Listener, error) {
	return (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
}

// serveCommand loads configuration, opens the database, applies migrations and
// only then starts the HTTP server. It returns when the signal-cancelled
// context ends, after in-flight requests drain.
var serveCommand = command{
	name:    "serve",
	summary: "run the web server",
	run: func(ctx context.Context, _ []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.Server.LogLevel})))
		return serve(ctx, cfg, db.Migrations(), tcpListen)
	},
}

// serve opens the database and applies the migrations in fsys. A failed
// migration returns before anything listens (ADR-00002 obligation 4).
func serve(ctx context.Context, cfg config.Config, migrations fs.FS, listen listenFunc) error {
	d, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := d.Close(); err != nil {
			slog.WarnContext(ctx, "close database", "error", err.Error())
		}
	}()
	if err := db.Migrate(ctx, d, migrations); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	backups, err := startBackups(ctx, cfg, d)
	if err != nil {
		return err
	}
	defer backups.Stop()
	// backups.Runner is the live job: hand it to anything that triggers a
	// backup or reads its status, never build a second Runner.

	deps := web.Deps{Config: cfg, DB: d, Backup: backups.Runner, BackupSecrets: backupSecrets(cfg), HealthDetails: settings.HealthDetails(backups.Runner)}
	var authOpts []auth.Option
	if cfg.OIDC.Configured() {
		if deps.Sessions, deps.SignIn, err = newSignIn(cfg, d); err != nil {
			return err
		}
		authOpts = append(authOpts, auth.WithSessions(deps.Sessions, layout.SignInPath))
	}
	if deps.Auth, err = auth.New(cfg, authOpts...); err != nil {
		return err
	}
	handler, err := web.New(deps)
	if err != nil {
		return err
	}
	ln, err := listen(ctx, cfg.Server.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", cfg.Server.ListenAddr, err)
	}
	slog.InfoContext(ctx, "listening", "addr", ln.Addr().String())
	return web.Serve(ctx, ln, handler)
}

// newSignIn builds the session store and sign-in from the configuration
// (ADR-00030). Call it only when sign-in is configured.
func newSignIn(cfg config.Config, d *sql.DB) (*session.Store, *signin.Service, error) {
	sessions, err := session.New(d, cfg.Session.Lifetime)
	if err != nil {
		return nil, nil, err
	}
	s, err := signin.New(signin.Settings{
		Issuer: cfg.OIDC.Issuer, ClientID: cfg.OIDC.ClientID, ClientSecret: cfg.OIDC.ClientSecret.Reveal(),
		PublicBaseURL: cfg.OIDC.PublicBaseURL, Lifetime: cfg.Session.Lifetime,
	}, sessions)
	if err != nil {
		return nil, nil, err
	}
	return sessions, s, nil
}
