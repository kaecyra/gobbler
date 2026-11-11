package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kaecyra/gobbler/internal/llmusage"
)

// priceSyncCheckEvery is how often the price sync asks whether it is due. The
// sync itself runs once a day (llmusage.SyncEvery); checking hourly retries a
// failed day within the hour, and a restart does not fetch again when the last
// success is recent.
const priceSyncCheckEvery = time.Hour

// priceSyncer is what the schedule calls: the store says whether a sync is due
// and the syncer runs it.
type priceSyncer interface {
	Due(ctx context.Context) (bool, error)
	Sync(ctx context.Context) error
}

// priceSyncJob is the daily model-price sync running in the background.
type priceSyncJob struct {
	stop func()
}

// Stop ends the schedule and waits for a sync in progress, so the database can
// close.
func (p priceSyncJob) Stop() { p.stop() }

// dueSyncer pairs the store's Due with the syncer's Sync.
type dueSyncer struct {
	store  *llmusage.Store
	syncer *llmusage.Syncer
}

func (d dueSyncer) Due(ctx context.Context) (bool, error) { return d.store.Due(ctx) }
func (d dueSyncer) Sync(ctx context.Context) error        { return d.syncer.Sync(ctx) }

// startPriceSync checks at start-up, and then every priceSyncCheckEvery until
// ctx ends or Stop is called, whether the model prices are due for a sync
// (ADR-00058). A sync that fails is logged and recorded by the syncer and tried
// again at the next check: prices are housekeeping nothing waits on.
func startPriceSync(ctx context.Context, s priceSyncer, every time.Duration) priceSyncJob {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			syncPricesIfDue(ctx, s)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return priceSyncJob{stop: func() { cancel(); wg.Wait() }}
}

func syncPricesIfDue(ctx context.Context, s priceSyncer) {
	due, err := s.Due(ctx)
	switch {
	case ctx.Err() != nil:
		return
	case err != nil:
		slog.ErrorContext(ctx, "llm price sync: could not tell whether it is due; trying again next time", "error", err.Error())
		return
	case !due:
		return
	}
	// The syncer logs and records its own failure.
	if err := s.Sync(ctx); err != nil && ctx.Err() == nil {
		slog.DebugContext(ctx, "llm price sync did not complete", "error", err.Error())
	}
}
