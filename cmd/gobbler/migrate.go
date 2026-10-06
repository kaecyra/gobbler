package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

// migrateCommand applies pending database migrations and exits. A failed
// migration is returned, so the process exits non-zero.
var migrateCommand = command{
	name:    "migrate",
	summary: "apply pending database migrations",
	run: func(ctx context.Context, _ []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return migrate(ctx, cfg, nil)
	},
}

// migrate opens the database and applies migrations from fsys, or the
// embedded set when fsys is nil.
func migrate(ctx context.Context, cfg config.Config, fsys fs.FS) error {
	d, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := d.Close(); err != nil {
			slog.WarnContext(ctx, "close database", "error", err.Error())
		}
	}()
	if fsys == nil {
		err = db.MigrateEmbedded(ctx, d)
	} else {
		err = db.Migrate(ctx, d, fsys)
	}
	if err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	return nil
}
