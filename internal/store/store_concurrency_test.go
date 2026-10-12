package store

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/catalog"
)

// Concurrent writers on one file database must queue on the write lock rather
// than fail: a transaction that reads before its first write cannot upgrade a
// stale snapshot and gets SQLITE_BUSY at once, ignoring the busy timeout.
func TestConcurrentCreatesDoNotFailBusy(t *testing.T) {
	s, d := newTestStore(t)
	s.now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) } // the test clock is not goroutine-safe
	ctx := authedCtx(t)

	const workers, each = 8, 25
	var wg sync.WaitGroup
	errs := make(chan error, workers*each)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if _, err := s.Create(ctx, simpleRecipe(fmt.Sprintf("w%d-%d", w, i))); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	failed := 0
	var first error
	for err := range errs {
		if failed == 0 {
			first = err
		}
		failed++
	}
	if failed > 0 {
		t.Errorf("%d of %d concurrent creates failed; first: %v", failed, workers*each, first)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM recipes`); n != workers*each-failed {
		t.Errorf("recipes = %d, want %d", n, workers*each-failed)
	}
}

// The same for the other write paths, which take the lock with lockRecipe.
func TestConcurrentEditsAndJournalDoNotFailBusy(t *testing.T) {
	s, _ := newTestStore(t)
	s.now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("shared"))
	if err != nil {
		t.Fatal(err)
	}

	const workers, each = 6, 10
	var wg sync.WaitGroup
	errs := make(chan error, workers*each*4)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if err := s.Update(ctx, id, simpleRecipe(fmt.Sprintf("w%d-%d", w, i))); err != nil {
					errs <- fmt.Errorf("Update: %w", err)
				}
				if _, err := s.AddNote(ctx, id, "n"); err != nil {
					errs <- fmt.Errorf("AddNote: %w", err)
				}
				if _, err := s.AddMadeIt(ctx, id, MadeIt{}); err != nil {
					errs <- fmt.Errorf("AddMadeIt: %w", err)
				}
				if _, err := s.Clone(ctx, id); err != nil {
					errs <- fmt.Errorf("Clone: %w", err)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// An ingredient merge runs the store's hooks inside its transaction, so it is
// a writer like any other: raced against recipe edits and against other
// merges on a file database it must queue on the write lock. A merge whose
// first statement is a read pins a snapshot and fails with SQLITE_BUSY when
// another writer commits before its first write (#59).
func TestConcurrentMergesAndEditsDoNotFailBusy(t *testing.T) {
	s, d := newTestStore(t)
	s.now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }
	c := catalog.New(d)
	s.RegisterMergeHooks(c)
	ctx := authedCtx(t)
	id, err := s.Create(ctx, simpleRecipe("shared"))
	if err != nil {
		t.Fatal(err)
	}

	const pairs, mergers = 40, 4
	type pair struct{ winner, loser int64 }
	work := make(chan pair, pairs)
	for i := 0; i < pairs; i++ {
		var p pair
		for _, o := range []struct {
			dst  *int64
			name string
		}{{&p.winner, "win"}, {&p.loser, "lose"}} {
			in, err := c.CreateIngredient(ctx, catalog.Ingredient{Name: fmt.Sprintf("zz merge %s %d", o.name, i), Aisle: "test"})
			if err != nil {
				t.Fatal(err)
			}
			*o.dst = in.ID
		}
		work <- p
	}
	close(work)

	var merging sync.WaitGroup
	var done atomic.Bool
	errs := make(chan error, pairs+1024)
	for m := 0; m < mergers; m++ {
		merging.Add(1)
		go func() {
			defer merging.Done()
			for p := range work {
				if _, err := c.MergeIngredients(ctx, p.winner, p.loser); err != nil {
					errs <- fmt.Errorf("MergeIngredients: %w", err)
				}
			}
		}()
	}
	var editing sync.WaitGroup
	for w := 0; w < 4; w++ {
		editing.Add(1)
		go func() {
			defer editing.Done()
			for i := 0; !done.Load(); i++ {
				if err := s.Update(ctx, id, simpleRecipe(fmt.Sprintf("w%d-%d", w, i))); err != nil {
					errs <- fmt.Errorf("Update: %w", err)
					return
				}
				if _, err := s.AddNote(ctx, id, "n"); err != nil {
					errs <- fmt.Errorf("AddNote: %w", err)
					return
				}
			}
		}()
	}
	merging.Wait()
	done.Store(true)
	editing.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM ingredients WHERE name LIKE 'zz merge lose %'`); n != 0 {
		t.Errorf("%d merged ingredients still exist", n)
	}
}
