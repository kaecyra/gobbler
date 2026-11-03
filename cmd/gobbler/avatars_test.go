package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/profile"
)

// A restart finds the jobs it interrupted and finishes them (ADR-00033).
func TestStartAvatarsResumesInterruptedJobs(t *testing.T) {
	ctx := testContext(t)
	cfg := serveConfig(t)
	d, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}

	// The previous process queued a job and stopped before running it.
	profiles := profile.New(d)
	local, err := media.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := profiles.Ensure(ctx, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	old := profile.NewAvatars(stopped, profiles, local, nil)
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "media", "testdata", "sample.heic"))
	if err != nil {
		t.Fatal(err)
	}
	j, err := old.Upload(ctx, "ann@example.com", data)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Crop(ctx, "ann@example.com", j.ID, media.Full); err != nil {
		t.Fatal(err)
	}
	old.Wait()

	a, err := startAvatars(ctx, cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	a.Wait()
	p, err := profiles.Ensure(ctx, "ann@example.com")
	if err != nil || p.Avatar == "" {
		t.Errorf("after start-up the profile has avatar %q, %v, want the resumed job's picture", p.Avatar, err)
	}
}
