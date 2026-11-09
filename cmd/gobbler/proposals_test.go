package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/picture"
	"github.com/kaecyra/gobbler/internal/store"
)

type countingSweeper struct {
	calls atomic.Int32
	err   error
}

func (c *countingSweeper) Sweep(context.Context) (store.SweepResult, error) {
	c.calls.Add(1)
	return store.SweepResult{}, c.err
}

// The sweep runs at start-up, then on every tick, keeps going after a failed
// sweep, and stops when asked.
func TestProposalSweepsRunAtStartAndPeriodicallyUntilStopped(t *testing.T) {
	s := &countingSweeper{err: errors.New("disk full")}
	sweeps := runProposalSweeps(testContext(t), s, 5*time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for s.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	sweeps.Stop()
	n := s.calls.Load()
	if n < 3 {
		t.Fatalf("%d sweeps ran, want a start-up sweep and periodic ones, failed or not", n)
	}
	time.Sleep(30 * time.Millisecond)
	if after := s.calls.Load(); after != n {
		t.Errorf("sweeps kept running after Stop: %d then %d", n, after)
	}
}

// The start-up sweep expires a proposal that has been pending for a year, in
// the real table.
func TestStartProposalSweepsExpiresOldProposals(t *testing.T) {
	ctx := testContext(t)
	d, err := db.Open(ctx, serveConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	if _, err := household.New(d).Create(ctx, "Home", "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	long := time.Now().Add(-store.PendingLifetime - time.Hour).UTC().Format(time.RFC3339Nano)
	_, err = d.ExecContext(ctx, `INSERT INTO proposals (household_id, token, kind, state, document, proposed_by, created_at, state_at)
		VALUES (1, 'old', 'draft', 'pending', '{}', 'ann@example.com', ?, ?)`, long, long)
	if err != nil {
		t.Fatal(err)
	}
	sweeps := startProposalSweeps(ctx, d, nil)
	t.Cleanup(sweeps.Stop)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if err := d.QueryRowContext(ctx, `SELECT state FROM proposals WHERE token = 'old'`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "expired" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("proposal is %s after the start-up sweep, want expired", state)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The start-up sweep deletes a picture offered on an edit page more than 14
// days ago, row and object (ADR-00051).
func TestStartProposalSweepsDeletesExpiredOfferedPictures(t *testing.T) {
	ctx := testContext(t)
	d, err := db.Open(ctx, serveConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	if _, err := household.New(d).Create(ctx, "Home", "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO recipes (id, household_id, name, created_at, created_by, updated_at, updated_by) VALUES (7, 1, 'Bread', 't', 'a', 't', 'a')`); err != nil {
		t.Fatal(err)
	}
	m := media.NewLocal(t.TempDir())
	const key = "recipes/7/found/AAAAAAAAAAAAAAAAAAAAAA.webp"
	if err := m.Put(ctx, key, []byte("webp"), "image/webp"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-picture.FoundLifetime - time.Hour).UTC().Format("2006-01-02T15:04:05.000000000Z")
	if _, err := d.ExecContext(ctx, `INSERT INTO recipe_found_pictures (household_id, recipe_id, offer, kind, source_url, key, width, height, found_at)
		VALUES (1, 7, 'AAAAAAAAAAAAAAAAAAAAAA', 'found', 'https://example.com/a.png', ?, 10, 10, ?)`, key, old); err != nil {
		t.Fatal(err)
	}
	sweeps := startProposalSweeps(ctx, d, picture.NewReaper(m, nil))
	t.Cleanup(sweeps.Stop)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		if err := d.QueryRowContext(ctx, `SELECT count(*) FROM recipe_found_pictures`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Get(ctx, key); n == 0 && errors.Is(err, media.ErrNotFound) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d offered rows left after the start-up sweep, want the expired one gone with its object", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
