package main

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/kaecyra/gobbler/internal/store"
)

// proposalSweepEvery is how often finished proposals are cleared out and old
// pending ones expire. A proposal's lifetimes are counted in days
// (store.PendingLifetime, store.FinishedLifetime), so an hour is far finer than
// they need and cheap: the sweep reads only the proposals table.
const proposalSweepEvery = time.Hour

// proposalSweeper is what the sweep calls on the store.
type proposalSweeper interface {
	Sweep(ctx context.Context) (store.SweepResult, error)
}

// proposalSweeps runs the proposal sweep in the background.
type proposalSweeps struct {
	stop func()
}

// Stop ends the sweeps and waits for one in progress, so the database can close.
func (p proposalSweeps) Stop() { p.stop() }

// startProposalSweeps sweeps the proposals table now, at start-up, and then
// every proposalSweepEvery until ctx ends or Stop is called (ADR-00035). A sweep
// that fails is logged and tried again at the next tick: it is housekeeping,
// nothing a person is waiting on, and an expired proposal that lingers an hour
// longer harms no one.
func startProposalSweeps(ctx context.Context, d *sql.DB) proposalSweeps {
	return runProposalSweeps(ctx, store.New(d), proposalSweepEvery)
}

func runProposalSweeps(ctx context.Context, s proposalSweeper, every time.Duration) proposalSweeps {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			sweepProposals(ctx, s)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return proposalSweeps{stop: func() { cancel(); wg.Wait() }}
}

func sweepProposals(ctx context.Context, s proposalSweeper) {
	res, err := s.Sweep(ctx)
	switch {
	case ctx.Err() != nil:
		return
	case err != nil:
		slog.ErrorContext(ctx, "proposal sweep failed; trying again next time", "error", err.Error())
	case res.Expired > 0 || res.Deleted > 0:
		slog.InfoContext(ctx, "proposal sweep", "expired", res.Expired, "deleted", res.Deleted)
	}
}
