package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakePriceSyncer struct {
	due     bool
	dueErr  error
	syncErr error
	syncs   atomic.Int32
	checks  atomic.Int32
}

func (f *fakePriceSyncer) Due(context.Context) (bool, error) {
	f.checks.Add(1)
	return f.due, f.dueErr
}

func (f *fakePriceSyncer) Sync(context.Context) error {
	f.syncs.Add(1)
	return f.syncErr
}

func waitForChecks(f *fakePriceSyncer, n int32) {
	deadline := time.Now().Add(5 * time.Second)
	for f.checks.Load() < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

// The check runs at start-up and on every tick, and a sync runs exactly when
// one is due, keeping on after a failed one, until stopped.
func TestPriceSyncRunsWhenDueAtStartAndPeriodically(t *testing.T) {
	f := &fakePriceSyncer{due: true, syncErr: errors.New("offline")}
	job := startPriceSync(testContext(t), f, 5*time.Millisecond)
	waitForChecks(f, 3)
	job.Stop()
	if f.checks.Load() < 3 || f.syncs.Load() != f.checks.Load() {
		t.Fatalf("checks = %d, syncs = %d, want a sync at every check while due", f.checks.Load(), f.syncs.Load())
	}
	n := f.checks.Load()
	time.Sleep(30 * time.Millisecond)
	if f.checks.Load() != n {
		t.Error("the schedule kept running after Stop")
	}
}

func TestPriceSyncDoesNotRunWhenNotDue(t *testing.T) {
	f := &fakePriceSyncer{due: false}
	job := startPriceSync(testContext(t), f, 5*time.Millisecond)
	waitForChecks(f, 3)
	job.Stop()
	if f.syncs.Load() != 0 {
		t.Errorf("%d syncs ran, want none: the last success is recent", f.syncs.Load())
	}
}

func TestPriceSyncDoesNotRunWhenDueCannotBeRead(t *testing.T) {
	f := &fakePriceSyncer{due: true, dueErr: errors.New("database is locked")}
	job := startPriceSync(testContext(t), f, 5*time.Millisecond)
	waitForChecks(f, 2)
	job.Stop()
	if f.syncs.Load() != 0 {
		t.Errorf("%d syncs ran after the due check failed", f.syncs.Load())
	}
}
