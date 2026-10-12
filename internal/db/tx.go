package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// Queries is the part of a sqlc-generated Queries type that the transaction
// helpers need: the generated WithTx method (ADR-00014). Q is the generated
// type itself, so a package passes its own `*catalogdb.Queries`.
type Queries[Q any] interface {
	WithTx(tx *sql.Tx) Q
}

// writeGates holds one gate per database handle: a channel of capacity one,
// so a queued writer can still give up when its context ends. Entries are
// never removed: production opens one database per process and tests one per
// test, so the map stays as small as the number of databases ever opened.
var writeGates sync.Map // *sql.DB -> chan struct{}

// acquireWrite waits for the write gate of d and returns the function that
// releases it.
func acquireWrite(ctx context.Context, d *sql.DB) (release func(), err error) {
	g, _ := writeGates.LoadOrStore(d, make(chan struct{}, 1))
	gate := g.(chan struct{})
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for write transaction: %w", ctx.Err())
	}
}

// WithTx runs fn in one transaction, rolling back when fn returns an error
// and committing otherwise. fn gets q bound to the transaction, and the
// transaction itself for callers that must hand it on (the catalog merge
// hooks). A failed rollback is joined onto the error that caused it.
//
// Write transactions on one *sql.DB run one at a time: WithTx waits for its
// turn in the process before it begins. SQLite allows one writer, and waiting
// on its busy handler is unfair: the handler polls, so under sustained
// contention a writer can starve past the busy timeout and fail with
// SQLITE_BUSY (#93). Queueing here makes the wait ordered and bounded by the
// transactions ahead rather than by the poll. The busy timeout remains for
// writers outside WithTx. fn must not call WithTx on the same database: that
// waits on itself.
//
// A transaction whose first statement is a read holds a snapshot, and when
// another writer outside this queue commits before its first write it cannot
// upgrade: it fails with SQLITE_BUSY at once, ignoring the busy timeout. fn
// that reads before it writes must therefore make its first statement a
// write (a no-op UPDATE of the row it is about to read is enough).
func WithTx[Q Queries[Q]](ctx context.Context, d *sql.DB, q Q, fn func(q Q, tx *sql.Tx) error) (err error) {
	release, err := acquireWrite(ctx, d)
	if err != nil {
		return err
	}
	defer release()
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, rollback(tx, "rollback"))
		}
	}()
	if err = fn(q.WithTx(tx), tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// WithReadTx runs fn against one read-only transaction, so every query in a
// multi-query read sees the same snapshot. fn gets q bound to the transaction,
// and the transaction itself for a query sqlc cannot express (as WithTx).
// Reading a document on the bare pool lets a write land between its queries and
// return a torn mix of two versions. Whatever fn returns, the transaction is
// ended.
func WithReadTx[Q Queries[Q]](ctx context.Context, d *sql.DB, q Q, fn func(q Q, tx *sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read transaction: %w", err)
	}
	err = fn(q.WithTx(tx), tx)
	return errors.Join(err, rollback(tx, "end read transaction"))
}

func rollback(tx *sql.Tx, what string) error {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// Constraint is a SQLite extended result code for a constraint violation.
type Constraint int

// The violations a caller can ask WrapWrite to recognise.
const (
	ConstraintUnique     = Constraint(sqlitelib.SQLITE_CONSTRAINT_UNIQUE)
	ConstraintPrimaryKey = Constraint(sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY)
	ConstraintForeignKey = Constraint(sqlitelib.SQLITE_CONSTRAINT_FOREIGNKEY)
)

// WrapWrite wraps a failed write with what it was doing. When err is one of
// the listed constraint violations the result also wraps sentinel, so callers
// branch on it with errors.Is; any other failure, including the other
// constraint kinds, is wrapped as it is. The driver reports the SQLite result
// code, so no message text is read. A nil err stays nil.
func WrapWrite(what string, err, sentinel error, on ...Constraint) error {
	if err == nil {
		return nil
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		for _, c := range on {
			if se.Code() == int(c) {
				return fmt.Errorf("%w: %s: %w", sentinel, what, err)
			}
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}
