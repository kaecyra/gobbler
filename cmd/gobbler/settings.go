package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/settings"
)

// openSettings builds the integration settings store (ADR-00057) and logs, at
// error level, every stored secret it cannot open. A missing, malformed or
// mismatched encryption key does not stop the app: those secrets read as
// unreadable and their integrations stay off until they are entered again. A
// malformed key is logged and treated as missing; its value is never logged.
func openSettings(ctx context.Context, cfg config.Config, d *sql.DB, log *slog.Logger) (*settings.Store, error) {
	var key []byte
	if raw := cfg.Settings.EncryptionKey.Reveal(); raw != "" {
		k, err := settings.ParseKey(raw)
		if err != nil {
			log.ErrorContext(ctx, "encryption key ignored; stored secrets are unreadable and secrets cannot be saved",
				"env", config.EnvEncryptionKey, "error", err.Error())
		} else {
			key = k
		}
	}
	store, err := settings.New(d, key)
	if err != nil {
		return nil, fmt.Errorf("open settings store: %w", err)
	}
	// The check only reports. If it cannot run, the app still starts, as it
	// does with a bad key: say so at error level and carry on.
	if _, err := store.LogUnreadable(ctx, log); err != nil {
		log.ErrorContext(ctx, "could not check stored secrets; they may be unreadable", "error", err.Error())
	}
	return store, nil
}
