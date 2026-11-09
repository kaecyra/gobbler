package main

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/store"
)

// fakeProposalSweep is the store's proposal sweep, answering res and err.
type fakeProposalSweep struct {
	res   store.SweepResult
	err   error
	calls int
}

func (f *fakeProposalSweep) SweepWithPictures(context.Context, store.ObjectRemover) (store.SweepResult, error) {
	f.calls++
	return f.res, f.err
}

// fakeFoundSweep is the picture reaper's sweep of offered pictures,
// answering n and err.
type fakeFoundSweep struct {
	n     int
	err   error
	calls int
}

func (f *fakeFoundSweep) Remove(context.Context, []string) {}

func (f *fakeFoundSweep) SweepFound(context.Context, *sql.DB, time.Time) (int, error) {
	f.calls++
	return f.n, f.err
}

// ADR-00051: the proposal sweep and the sweep of offered pictures each run
// whatever the other did, and a failure of either is returned, both when both
// fail.
func TestTheCombinedSweepRunsBothAndJoinsTheirErrors(t *testing.T) {
	proposalErr := errors.New("proposals table locked")
	foundErr := errors.New("media store down")
	for name, tc := range map[string]struct {
		proposalErr, foundErr error
	}{
		"both succeed":          {},
		"the proposals fail":    {proposalErr: proposalErr},
		"the found sweep fails": {foundErr: foundErr},
		"both fail":             {proposalErr: proposalErr, foundErr: foundErr},
	} {
		t.Run(name, func(t *testing.T) {
			proposals := &fakeProposalSweep{res: store.SweepResult{Expired: 2, Deleted: 1}, err: tc.proposalErr}
			found := &fakeFoundSweep{n: 3, err: tc.foundErr}
			res, err := pictureSweeper{proposals: proposals, pictures: found}.Sweep(context.Background())
			if proposals.calls != 1 || found.calls != 1 {
				t.Errorf("proposal sweep ran %d times, found sweep %d; want each once", proposals.calls, found.calls)
			}
			if res != proposals.res {
				t.Errorf("result = %+v, want the proposal sweep's %+v", res, proposals.res)
			}
			for _, want := range []error{tc.proposalErr, tc.foundErr} {
				if want != nil && !errors.Is(err, want) {
					t.Errorf("err = %v, want it to carry %v", err, want)
				}
			}
			if tc.proposalErr == nil && tc.foundErr == nil && err != nil {
				t.Errorf("err = %v, want none", err)
			}
		})
	}
}
