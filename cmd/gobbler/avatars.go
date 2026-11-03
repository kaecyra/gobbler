package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/profile"
)

// startAvatars builds the avatar service over the configured media store and
// runs again every job a restart interrupted (ADR-00033). Its jobs end with
// ctx; the caller waits for them with Wait before closing the database.
//
// Failing to list the interrupted jobs does not stop the server: avatars are
// not what the app is for, the jobs stay queued in the database, and the next
// start tries again. The failure is logged at error level.
func startAvatars(ctx context.Context, cfg config.Config, d *sql.DB) (*profile.Avatars, error) {
	m, err := media.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("open media storage: %w", err)
	}
	a := profile.NewAvatars(ctx, profile.New(d), m, slog.Default())
	if err := a.Resume(ctx); err != nil {
		slog.ErrorContext(ctx, "resume avatar jobs; interrupted uploads wait for the next start", "error", err.Error())
	}
	return a, nil
}
