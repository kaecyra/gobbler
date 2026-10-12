package backup

import (
	"context"
	"fmt"
	"time"
)

// NextRun is the first occurrence of the "HH:MM" time of day strictly after
// now, in now's location.
func NextRun(now time.Time, hhmm string) (time.Time, error) {
	tod, err := time.Parse("15:04", hhmm)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid backup schedule %q (want HH:MM, 24-hour): %w", hhmm, err)
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), tod.Hour(), tod.Minute(), 0, 0, now.Location())
	if !next.After(now) {
		next = time.Date(now.Year(), now.Month(), now.Day()+1, tod.Hour(), tod.Minute(), 0, 0, now.Location())
	}
	return next, nil
}

// ScheduleDeps are the seams Schedule needs: the clock, a cancellable sleep
// and the job.
type ScheduleDeps struct {
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	Run   func(ctx context.Context) error
}

// Schedule runs the job once a day at hhmm until ctx ends, then returns the
// context's error. A failed run does not stop the schedule: one bad night must
// not end the backups after it. The job logs its own failures.
func Schedule(ctx context.Context, hhmm string, deps ScheduleDeps) error {
	for {
		next, err := NextRun(deps.Now(), hhmm)
		if err != nil {
			return err
		}
		if err := deps.Sleep(ctx, next.Sub(deps.Now())); err != nil {
			return err
		}
		// Runner.Run has already logged a failure with its details; logging it
		// again here would double every failed night. Either way the schedule
		// continues.
		_ = deps.Run(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// SleepContext waits for d or until ctx ends, returning the context's error in
// the second case.
func SleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
