// Package db provides the SQLite connection and the migration runner. SQL for
// features lives in their own store packages; this package knows only how to
// open the file and bring its schema up to date (ADR-00002).
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"

	"github.com/kaecyra/gobbler/internal/config"
)

// FileName is the SQLite file inside the configured data directory.
const FileName = "gobbler.db"

const busyTimeoutMillis = 5000

//go:embed migrations
var embedded embed.FS

// Open opens the database in cfg.Server.DataDir, creating the directory and
// file if missing. WAL journal mode, foreign keys and a busy timeout are set
// on every pooled connection through the DSN.
func Open(ctx context.Context, cfg config.Config) (*sql.DB, error) {
	dir := cfg.Server.DataDir
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory %q: %w", dir, err)
	}
	path := filepath.Join(dir, FileName)

	q := url.Values{}
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMillis))
	dsn := (&url.URL{Scheme: "file", Opaque: filepath.ToSlash(path), RawQuery: q.Encode()}).String()

	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database %q: %w", path, err)
	}
	if err := d.PingContext(ctx); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("connect to database %q: %w", path, err)
	}
	return d, nil
}

// MigrateEmbedded applies the migrations compiled into the binary. The serve
// path calls it at start-up and must not proceed if it returns an error.
func MigrateEmbedded(ctx context.Context, d *sql.DB) error {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	return Migrate(ctx, d, sub)
}

// Migrate applies every pending goose migration found at the root of fsys.
// A failure is returned wrapped with the migration's file name; earlier
// migrations stay applied. No migrations at all is not an error.
func Migrate(ctx context.Context, d *sql.DB, fsys fs.FS) error {
	p, err := goose.NewProvider(goose.DialectSQLite3, d, fsys)
	if errors.Is(err, goose.ErrNoMigrations) {
		slog.InfoContext(ctx, "no migrations to apply")
		return nil
	}
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}
	results, err := p.Up(ctx)
	for _, r := range results {
		if r.Error == nil {
			slog.InfoContext(ctx, "applied migration", "migration", r.Source.Path, "version", r.Source.Version)
		}
	}
	if err != nil {
		var pe *goose.PartialError
		if errors.As(err, &pe) && pe.Failed != nil {
			return fmt.Errorf("apply migration %s: %w", pe.Failed.Source.Path, pe.Err)
		}
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Version returns the highest applied migration version, or 0 on a fresh
// database or when fsys holds no migrations.
func Version(ctx context.Context, d *sql.DB, fsys fs.FS) (int64, error) {
	p, err := goose.NewProvider(goose.DialectSQLite3, d, fsys)
	if errors.Is(err, goose.ErrNoMigrations) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("prepare migrations: %w", err)
	}
	v, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read migration version: %w", err)
	}
	return v, nil
}
