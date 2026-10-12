package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaecyra/gobbler/internal/backup"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

func TestBackupNowWritesLocalSnapshot(t *testing.T) {
	cfg := config.Config{}
	cfg.Server.DataDir = t.TempDir()
	cfg.Backup = config.Backup{Schedule: "03:30", LocalRetention: 3, RemoteRetention: 3}

	if err := backupNow(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.Server.DataDir, backupDirName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !backup.IsSnapshotName(entries[0].Name()) {
		t.Fatalf("backup dir holds %v, want one snapshot", entries)
	}
}

func TestStartBackupsStopsWithContext(t *testing.T) {
	cfg := config.Config{}
	cfg.Server.DataDir = t.TempDir()
	cfg.Backup = config.Backup{Schedule: "03:30", LocalRetention: 3, RemoteRetention: 3}
	ctx, cancel := context.WithCancel(context.Background())
	d, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	wait, err := startBackups(ctx, cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	wait() // ctx is still live: wait must stop the schedule itself, not hang
}

func TestStartBackupsRejectsBadEndpoint(t *testing.T) {
	cfg := config.Config{}
	cfg.Backup.S3.Endpoint = "ftp://x"
	if _, err := startBackups(context.Background(), cfg, nil); err == nil {
		t.Fatal("want error for unsupported endpoint scheme")
	}
}
