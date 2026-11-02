package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/session/sessiondb"
)

// ErrNoSession is returned (wrapped) by Lookup when the token names no live
// session: it was never issued, it was deleted, or it has expired. The three
// are one error, so the caller treats them alike and asks the person to sign
// in.
var ErrNoSession = errors.New("no session")

// tokenBytes is the size of a session token before encoding: 256 bits from
// a cryptographically secure source.
const tokenBytes = 32

// Fixed width, so text comparison in SQL orders timestamps correctly.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// encoding turns a token into cookie-safe text.
var encoding = base64.RawURLEncoding

// Store creates, finds and ends sessions. It is safe for concurrent use.
type Store struct {
	db       *sql.DB
	q        *sessiondb.Queries
	lifetime time.Duration
	now      func() time.Time
	// rand is where session tokens are drawn from.
	rand io.Reader
}

// New returns a Store over d, which must already be migrated. Every session
// it creates ends lifetime after sign-in (config.Session.Lifetime), which
// must be positive.
func New(d *sql.DB, lifetime time.Duration) (*Store, error) {
	if lifetime <= 0 {
		return nil, fmt.Errorf("session lifetime %s must be positive", lifetime)
	}
	return &Store{db: d, q: sessiondb.New(d), lifetime: lifetime, now: time.Now, rand: rand.Reader}, nil
}

// hash is the only form of a token the database sees.
func hash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func (s *Store) stamp(t time.Time) string { return t.UTC().Format(timeLayout) }

// Create starts a session for the verified email and returns its token, for
// the cookie and nowhere else: it is not stored, and must never be logged.
// The email is trimmed and lower-cased, the form every table stores.
func (s *Store) Create(ctx context.Context, email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return "", errors.New("create session: no email")
	}
	raw := make([]byte, tokenBytes)
	if _, err := io.ReadFull(s.rand, raw); err != nil {
		return "", fmt.Errorf("draw session token: %w", err)
	}
	token := encoding.EncodeToString(raw)
	now := s.now()
	err := db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
		return q.InsertSession(ctx, sessiondb.InsertSessionParams{
			TokenHash: hash(token), Email: e, CreatedAt: s.stamp(now), LastUsedAt: s.stamp(now),
			ExpiresAt: s.stamp(now.Add(s.lifetime)),
		})
	})
	// A primary-key clash on a 256-bit random token means the random source
	// is broken, so it is reported as the failure it is, not redrawn.
	if err != nil {
		return "", fmt.Errorf("insert session for %s: %w", e, err)
	}
	return token, nil
}

// Lookup returns the email of the live session the token names, and records
// that it was used. It never extends the session (ADR-00030). An unknown,
// deleted, expired or malformed token is ErrNoSession; an expired session's
// row is deleted on the way. No error it returns contains the token.
func (s *Store) Lookup(ctx context.Context, token string) (string, error) {
	if b, err := encoding.DecodeString(token); err != nil || len(b) != tokenBytes {
		return "", fmt.Errorf("look up session: malformed token: %w", ErrNoSession)
	}
	h := hash(token)
	var email string
	found := false
	err := db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
		now := s.stamp(s.now())
		var err error
		email, err = q.UseSession(ctx, sessiondb.UseSessionParams{Now: now, TokenHash: h})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// Not an error inside the transaction: returning one would roll
			// back the delete of an expired row.
			if err := q.DeleteExpiredSession(ctx, sessiondb.DeleteExpiredSessionParams{TokenHash: h, Now: now}); err != nil {
				return fmt.Errorf("delete expired session: %w", err)
			}
			return nil
		case err != nil:
			return fmt.Errorf("look up session: %w", err)
		}
		found = true
		return nil
	})
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("look up session: %w", ErrNoSession)
	}
	return email, nil
}

// Delete ends the session the token names: signing out. A token that names
// no session is not an error, so signing out twice is harmless.
func (s *Store) Delete(ctx context.Context, token string) error {
	err := db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
		return q.DeleteSession(ctx, hash(token))
	})
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteForEmail ends every session of the email, as when a member is
// removed, and returns how many it ended. The email is matched in the form
// Create stores it.
func (s *Store) DeleteForEmail(ctx context.Context, email string) (int, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	var n int64
	err := db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.DeleteSessionsForEmail(ctx, e)
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("delete sessions of %s: %w", e, err)
	}
	return int(n), nil
}

// DeleteExpired removes every expired session and returns how many. Lookup
// already refuses them; this is housekeeping, so sessions nobody returns to do
// not accumulate.
func (s *Store) DeleteExpired(ctx context.Context) (int, error) {
	var n int64
	err := db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.DeleteExpiredSessions(ctx, s.stamp(s.now()))
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return int(n), nil
}
