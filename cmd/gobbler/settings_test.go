package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/settings"
)

func b64key(b byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, settings.KeyBytes))
}

// loadCfg loads a dev-mode configuration over a fresh data directory with the
// given encryption key variable.
func loadCfg(t *testing.T, dataDir, key string) config.Config {
	t.Helper()
	t.Setenv(config.EnvDataDir, dataDir)
	t.Setenv(config.EnvDevAuthBypass, "true")
	t.Setenv(config.EnvListenAddr, "127.0.0.1:0")
	t.Setenv(config.EnvEncryptionKey, key)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func migrated(t *testing.T, cfg config.Config) *sql.DB {
	t.Helper()
	d, err := db.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(t.Context(), d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	return d
}

// Stored secrets with no encryption key, or the wrong one, do not stop the
// app: they read as unreadable, and the failure is logged at error level
// without the value (ADR-00057 obligation 4).
func TestOpenSettingsWithStoredSecretsAndNoOrWrongKey(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	field := settings.Field{Integration: "mail", Name: "password"}
	cfg := loadCfg(t, dir, b64key(1))
	d := migrated(t, cfg)
	first, err := openSettings(ctx, cfg, d, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.SetSecret(ctx, field, "topsecretvalue"); err != nil {
		t.Fatal(err)
	}

	for name, key := range map[string]string{
		"no key":        "",
		"wrong key":     b64key(2),
		"malformed key": "not-base64!",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := loadCfg(t, dir, key)
			var buf bytes.Buffer
			store, err := openSettings(ctx, cfg, d, slog.New(slog.NewJSONHandler(&buf, nil)))
			if err != nil {
				t.Fatalf("openSettings = %v, want a start", err)
			}
			if _, err := store.Secret(ctx, field); !errors.Is(err, settings.ErrUnreadable) {
				t.Fatalf("Secret err = %v, want ErrUnreadable", err)
			}
			out := buf.String()
			if !strings.Contains(out, `"level":"ERROR"`) || !strings.Contains(out, `"field":"password"`) {
				t.Errorf("no error log naming the secret: %q", out)
			}
			if strings.Contains(out, "topsecretvalue") || (key != "" && strings.Contains(out, key)) {
				t.Errorf("the log holds a secret or the key: %q", out)
			}
		})
	}
}

func TestOpenSettingsQuietWithTheRightKey(t *testing.T) {
	ctx := context.Background()
	cfg := loadCfg(t, t.TempDir(), b64key(1))
	d := migrated(t, cfg)
	store, err := openSettings(ctx, cfg, d, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecret(ctx, settings.Field{Integration: "mail", Name: "password"}, "x"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := openSettings(ctx, cfg, d, slog.New(slog.NewJSONHandler(&buf, nil))); err != nil || buf.Len() != 0 {
		t.Fatalf("err = %v, log = %q; want a quiet start", err, buf.String())
	}
}

// serve itself starts, up to listening, with stored secrets and a wrong key.
func TestServeStartsWithUnreadableSecrets(t *testing.T) {
	dir := t.TempDir()
	cfg := loadCfg(t, dir, b64key(1))
	d := migrated(t, cfg)
	s, err := openSettings(t.Context(), cfg, d, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret(t.Context(), settings.Field{Integration: "mail", Name: "password"}, "x"); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()

	stop := errors.New("listening")
	listen := func(context.Context, string) (net.Listener, error) { return nil, stop }
	err = serve(testContext(t), loadCfg(t, dir, b64key(2)), db.Migrations(), listen)
	if !errors.Is(err, stop) {
		t.Fatalf("serve = %v, want it to reach listen", err)
	}
}
