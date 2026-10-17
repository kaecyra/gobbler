package db

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// A write transaction that reads before it writes holds a read snapshot. When
// a writer outside the WithTx queue commits in between, the upgrade fails with
// SQLITE_BUSY at once and the busy timeout never applies (#209). WithTx must
// take the write lock when it begins, so the outside writer waits instead.
func TestWithTxReadingFirstSurvivesAnUnqueuedWriter(t *testing.T) {
	d := txTestDB(t)
	ctx := context.Background()

	outsider := make(chan error, 1)
	err := WithTx(ctx, d, fakeQueries{}, func(_ fakeQueries, tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&n); err != nil {
			return err
		}
		// A writer on the bare pool, as the household invitations are.
		go func() {
			_, err := d.ExecContext(ctx, `INSERT INTO t (v) VALUES ('outsider')`)
			outsider <- err
		}()
		time.Sleep(300 * time.Millisecond)
		_, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES ('queued')`)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx failed beside an unqueued writer: %v", err)
	}
	if err := <-outsider; err != nil {
		t.Fatalf("unqueued writer failed: %v", err)
	}
	if got := rowCount(t, d); got != 2 {
		t.Errorf("rows = %d, want 2", got)
	}
}

// WithReadTx must not take the write lock: readers run beside a writer.
func TestWithReadTxDoesNotBlockOnAWriter(t *testing.T) {
	d := txTestDB(t)
	ctx := context.Background()
	err := WithTx(ctx, d, fakeQueries{}, func(_ fakeQueries, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO t (v) VALUES ('a')`); err != nil {
			return err
		}
		return WithReadTx(ctx, d, fakeQueries{}, func(_ fakeQueries, rtx *sql.Tx) error {
			var n int
			return rtx.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&n)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}
