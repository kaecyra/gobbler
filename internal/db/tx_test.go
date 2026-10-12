package db

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeQueries stands in for a generated Queries type.
type fakeQueries struct{ tx *sql.Tx }

func (fakeQueries) WithTx(tx *sql.Tx) fakeQueries { return fakeQueries{tx: tx} }

func txTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d := openTest(t)
	if _, err := d.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT NOT NULL UNIQUE, p INTEGER REFERENCES t (id))`); err != nil {
		t.Fatal(err)
	}
	return d
}

func rowCount(t *testing.T, d *sql.DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWithTxCommitsAndBindsQueries(t *testing.T) {
	d := txTestDB(t)
	err := WithTx(context.Background(), d, fakeQueries{}, func(q fakeQueries, tx *sql.Tx) error {
		if q.tx != tx {
			t.Error("queries are not bound to the transaction handed to fn")
		}
		_, err := tx.Exec(`INSERT INTO t (v) VALUES ('a')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := rowCount(t, d); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	d := txTestDB(t)
	boom := errors.New("boom")
	err := WithTx(context.Background(), d, fakeQueries{}, func(_ fakeQueries, tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO t (v) VALUES ('a')`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if n := rowCount(t, d); n != 0 {
		t.Errorf("rows = %d after rollback, want 0", n)
	}
}

func TestWithTxReportsFailedBegin(t *testing.T) {
	d := txTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := WithTx(ctx, d, fakeQueries{}, func(fakeQueries, *sql.Tx) error { called = true; return nil })
	if err == nil || called {
		t.Errorf("err = %v, called = %v; want a begin error and no call", err, called)
	}
}

func TestWithReadTxBindsQueriesAndEndsTheTransaction(t *testing.T) {
	d := txTestDB(t)
	var bound *sql.Tx
	err := WithReadTx(context.Background(), d, fakeQueries{}, func(q fakeQueries) error {
		bound = q.tx
		var n int
		return q.tx.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Exec(`SELECT 1`); !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("transaction still open after WithReadTx: %v", err)
	}
}

func TestWithReadTxReturnsFnError(t *testing.T) {
	d := txTestDB(t)
	boom := errors.New("boom")
	err := WithReadTx(context.Background(), d, fakeQueries{}, func(fakeQueries) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}

func TestWrapWriteMapsOnlyTheNamedConstraints(t *testing.T) {
	d := txTestDB(t)
	if _, err := d.Exec(`INSERT INTO t (v) VALUES ('dup')`); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("sentinel")
	cases := []struct {
		name  string
		query string
		on    []Constraint
		want  bool
	}{
		{"unique named", `INSERT INTO t (v) VALUES ('dup')`, []Constraint{ConstraintUnique}, true},
		{"unique not named", `INSERT INTO t (v) VALUES ('dup')`, []Constraint{ConstraintForeignKey}, false},
		{"foreign key named", `INSERT INTO t (v, p) VALUES ('x', 999)`, []Constraint{ConstraintForeignKey}, true},
		{"not null", `INSERT INTO t (v) VALUES (NULL)`, []Constraint{ConstraintUnique, ConstraintPrimaryKey, ConstraintForeignKey}, false},
		{"primary key named", `INSERT INTO t (id, v) VALUES (1, 'y'), (1, 'z')`, []Constraint{ConstraintPrimaryKey}, true},
	}
	for _, c := range cases {
		_, err := d.Exec(c.query)
		if err == nil {
			t.Fatalf("%s: statement unexpectedly succeeded", c.name)
		}
		got := WrapWrite("what", err, sentinel, c.on...)
		if errors.Is(got, sentinel) != c.want {
			t.Errorf("%s: errors.Is(sentinel) = %v, want %v (%v)", c.name, !c.want, c.want, got)
		}
		if !errors.Is(got, err) {
			t.Errorf("%s: wrapped error lost the cause", c.name)
		}
	}
	if WrapWrite("what", nil, sentinel, ConstraintUnique) != nil {
		t.Error("nil error must stay nil")
	}
}

// Write transactions on one database queue in the process rather than race on
// SQLite's busy handler, which polls and is not fair: under sustained
// contention a writer can starve past the busy timeout and fail (#93).
func TestWithTxRunsWritersOneAtATime(t *testing.T) {
	d := txTestDB(t)
	var running atomic.Int32
	var peak atomic.Int32
	var peakMu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := WithTx(context.Background(), d, fakeQueries{}, func(fakeQueries, *sql.Tx) error {
				n := running.Add(1)
				peakMu.Lock()
				if n > peak.Load() {
					peak.Store(n)
				}
				peakMu.Unlock()
				time.Sleep(5 * time.Millisecond)
				running.Add(-1)
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Errorf("peak concurrent write transactions = %d, want 1", peak.Load())
	}
}

func TestWithTxQueuedWriterHonoursContext(t *testing.T) {
	d := txTestDB(t)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- WithTx(context.Background(), d, fakeQueries{}, func(fakeQueries, *sql.Tx) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := WithTx(ctx, d, fakeQueries{}, func(fakeQueries, *sql.Tx) error {
		t.Error("fn ran while another write transaction was open")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
