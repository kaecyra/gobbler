package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/kaecyra/gobbler/internal/backup"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

// backupDirName is the snapshot directory inside the data directory.
const backupDirName = "backups"

// backupCommand runs the nightly backup job once, now (ADR-00010).
var backupCommand = command{
	name:    "backup",
	summary: "snapshot the database now, upload it and apply retention",
	run: func(ctx context.Context, args []string) error {
		fs := flag.NewFlagSet("backup", flag.ContinueOnError)
		if err := fs.Parse(args); err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		return backupNow(ctx, cfg)
	},
}

func backupNow(ctx context.Context, cfg config.Config) (err error) {
	d, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := d.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("close database: %w", cerr))
		}
	}()
	r, err := newBackupRunner(cfg, d)
	if err != nil {
		return err
	}
	_, err = r.Run(ctx)
	return err
}

// newBackupRunner wires the job from configuration. Without an S3 endpoint
// snapshots stay local.
func newBackupRunner(cfg config.Config, d *sql.DB) (*backup.Runner, error) {
	r := &backup.Runner{
		DB:         d,
		Dir:        filepath.Join(cfg.Server.DataDir, backupDirName),
		LocalKeep:  cfg.Backup.LocalRetention,
		RemoteKeep: cfg.Backup.RemoteRetention,
	}
	if cfg.Backup.S3.Configured() {
		store, err := backup.NewS3Store(cfg.Backup.S3)
		if err != nil {
			return nil, err
		}
		r.Store = store
		r.Prefix = backup.KeyPrefix(cfg.Backup.S3.Prefix)
	}
	return r, nil
}

// startBackups runs the daily schedule in the background until ctx ends. The
// returned function stops the schedule and waits for it to exit; call it before
// closing the database, including on error paths where ctx is still live. A failed night is logged by the job and does not stop the app.
func startBackups(ctx context.Context, cfg config.Config, d *sql.DB) (wait func(), err error) {
	r, err := newBackupRunner(cfg, d)
	if err != nil {
		return nil, err
	}
	if !cfg.Backup.S3.Configured() {
		slog.InfoContext(ctx, "no S3 endpoint configured; backups stay local", "dir", r.Dir)
	}
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := backup.Schedule(ctx, cfg.Backup.Schedule, backup.ScheduleDeps{
			Now:   func() time.Time { return time.Now() },
			Sleep: backup.SleepContext,
			Run:   func(ctx context.Context) error { _, err := r.Run(ctx); return err },
		})
		if !errors.Is(err, context.Canceled) { // Schedule never returns nil
			slog.ErrorContext(ctx, "backup schedule stopped", "error", err.Error())
		}
	}()
	return func() { cancel(); wg.Wait() }, nil
}
