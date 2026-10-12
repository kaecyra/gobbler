package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	var snaps int
	for _, e := range entries {
		if backup.IsSnapshotName(e.Name()) {
			snaps++
		}
	}
	if snaps != 1 {
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

	svc, err := startBackups(ctx, cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	svc.Stop() // ctx is still live: wait must stop the schedule itself, not hang
}

func TestStartBackupsRejectsBadEndpoint(t *testing.T) {
	cfg := config.Config{}
	cfg.Backup.S3.Endpoint = "ftp://x"
	if _, err := startBackups(context.Background(), cfg, nil); err == nil {
		t.Fatal("want error for unsupported endpoint scheme")
	}
}

// The scheduler must drive the same Runner the service exposes: a second
// Runner would split status and defeat the run lock.
func TestScheduledRunUsesTheExposedRunner(t *testing.T) {
	cfg := config.Config{}
	cfg.Server.DataDir = t.TempDir()
	cfg.Backup = config.Backup{Schedule: "03:30", LocalRetention: 3, RemoteRetention: 3}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	r, err := newBackupRunner(cfg, d)
	if err != nil {
		t.Fatal(err)
	}

	slept := false
	svc := runSchedule(ctx, r, cfg.Backup.Schedule, backup.ScheduleDeps{
		Now: time.Now,
		Sleep: func(ctx context.Context, _ time.Duration) error {
			if !slept { // fire once immediately, then wait for shutdown
				slept = true
				return nil
			}
			<-ctx.Done()
			return ctx.Err()
		},
	})
	defer svc.Stop()
	if svc.Runner != r {
		t.Fatal("service exposes a different Runner than the one scheduled")
	}

	statusFile := filepath.Join(r.Dir, "status.json")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(statusFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduled run never completed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Remove the persisted copy so only the Runner's live state can answer: a
	// different Runner over this directory would report no success.
	if err := os.Remove(statusFile); err != nil {
		t.Fatal(err)
	}
	if svc.Runner.Status().LastSuccess.IsZero() {
		t.Fatal("the exposed Runner has no record of the scheduled run")
	}
}
