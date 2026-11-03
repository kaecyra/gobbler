package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
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

// parse reads a time stamp written by stamp.
func (s *Store) parse(v string) (time.Time, error) { return time.Parse(timeLayout, v) }

// Create starts a session for the verified email and returns its token, for
// the cookie and nowhere else: it is not stored, and must never be logged.
// The email is trimmed and lower-cased, the form every table stores. The
// client is recorded with the session for the person to see (ADR-00032).
func (s *Store) Create(ctx context.Context, email string, c Client) (string, error) {
	e := normalise(email)
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
			ExpiresAt: s.stamp(now.Add(s.lifetime)), UserAgent: c.UserAgent, ClientIp: c.IP,
		})
	})
	// A primary-key clash on a 256-bit random token means the random source
	// is broken, so it is reported as the failure it is, not redrawn.
	if err != nil {
		return "", fmt.Errorf("insert session for %s: %w", e, err)
	}
	return token, nil
}

// touchEvery is how stale last_used_at may get before Lookup writes it again.
// Every private request looks its session up, and SQLite has one writer, so
// writing on each would queue page loads behind any long write; ADR-00030 asks
// for when a session was last used, not for every use.
const touchEvery = time.Minute

// Lookup returns the email of the live session the token names, and records
// that it was used, at most once per touchEvery. It never extends the session
// (ADR-00030). An unknown, deleted, expired or malformed token is
// ErrNoSession; an expired session's row is deleted on the way. No error it
// returns contains the token.
func (s *Store) Lookup(ctx context.Context, token string) (string, error) {
	if b, err := encoding.DecodeString(token); err != nil || len(b) != tokenBytes {
		return "", fmt.Errorf("look up session: malformed token: %w", ErrNoSession)
	}
	h := hash(token)
	now := s.now()
	// One SELECT reads one snapshot, so it needs no transaction (household
	// reads single rows the same way).
	row, err := s.q.GetLiveSession(ctx, sessiondb.GetLiveSessionParams{TokenHash: h, Now: s.stamp(now)})
	found := err == nil
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return "", fmt.Errorf("look up session: %w", err)
	}
	if !found {
		// An expired row, if that is what this was, is of no further use.
		err := db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
			return q.DeleteExpiredSession(ctx, sessiondb.DeleteExpiredSessionParams{TokenHash: h, Now: s.stamp(now)})
		})
		if err != nil {
			return "", fmt.Errorf("delete expired session: %w", err)
		}
		return "", fmt.Errorf("look up session: %w", ErrNoSession)
	}
	used, err := s.parse(row.LastUsedAt)
	if err != nil {
		return "", fmt.Errorf("look up session: last used: %w", err)
	}
	if now.Sub(used) >= touchEvery {
		err := db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
			return q.TouchSession(ctx, sessiondb.TouchSessionParams{Now: s.stamp(now), TokenHash: h})
		})
		if err != nil {
			return "", fmt.Errorf("record session use: %w", err)
		}
	}
	return row.Email, nil
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
	var n int
	err := db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
		var err error
		n, err = deleteForEmail(ctx, q, email)
		return err
	})
	return n, err
}

// EndForEmail is DeleteForEmail inside a transaction someone else owns. It is
// the hook that makes removing a member end their sessions at once
// (ADR-00030): the router registers it with household.Store.OnMemberLeave, so
// a failure here rolls the removal back. household's hook type is matched by
// signature, so neither store imports the other.
func (s *Store) EndForEmail(ctx context.Context, tx *sql.Tx, email string) error {
	_, err := deleteForEmail(ctx, s.q.WithTx(tx), email)
	return err
}

func deleteForEmail(ctx context.Context, q *sessiondb.Queries, email string) (int, error) {
	e := normalise(email)
	if e == "" {
		// No session has an empty email, so this can only be a caller that
		// lost the address it meant to end sessions for.
		return 0, errors.New("delete sessions: empty email")
	}
	n, err := q.DeleteSessionsForEmail(ctx, e)
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

// Info is one live session as its person sees it (ADR-00032). It holds no
// token: ID names the session to End, and opens nothing.
type Info struct {
	// ID is the SHA-256 of the session token in hex, which is what the
	// database stores and not the token itself.
	ID         string
	Client     Client
	CreatedAt  time.Time
	LastUsedAt time.Time
	// Current is whether this is the session the list was asked for.
	Current bool
}

// ListForEmail returns the email's live sessions, newest first, with the one
// the current token names marked. An empty or unknown token marks none.
func (s *Store) ListForEmail(ctx context.Context, email, currentToken string) ([]Info, error) {
	e := normalise(email)
	rows, err := s.q.ListLiveSessionsForEmail(ctx, sessiondb.ListLiveSessionsForEmailParams{Email: e, Now: s.stamp(s.now())})
	if err != nil {
		return nil, fmt.Errorf("list sessions of %s: %w", e, err)
	}
	cur := ""
	if currentToken != "" {
		cur = hex.EncodeToString(hash(currentToken))
	}
	out := make([]Info, 0, len(rows))
	for _, r := range rows {
		created, err := s.parse(r.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("list sessions of %s: created: %w", e, err)
		}
		used, err := s.parse(r.LastUsedAt)
		if err != nil {
			return nil, fmt.Errorf("list sessions of %s: last used: %w", e, err)
		}
		id := hex.EncodeToString(r.TokenHash)
		out = append(out, Info{
			ID: id, Client: Client{UserAgent: r.UserAgent, IP: r.ClientIp},
			CreatedAt: created, LastUsedAt: used, Current: cur != "" && id == cur,
		})
	}
	return out, nil
}

// End ends the email's session that id names and reports whether there was
// one. A session of another email is never touched, whatever the id: the
// email comes from the verified identity, the id from the request.
func (s *Store) End(ctx context.Context, email, id string) (bool, error) {
	h, err := hex.DecodeString(id)
	if err != nil || len(h) != sha256.Size {
		return false, nil
	}
	var n int64
	err = db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.DeleteSessionOfEmail(ctx, sessiondb.DeleteSessionOfEmailParams{TokenHash: h, Email: normalise(email)})
		return err
	})
	if err != nil {
		return false, fmt.Errorf("end session of %s: %w", normalise(email), err)
	}
	return n > 0, nil
}

// EndOthers ends every session of the email except the one the token names and
// returns how many it ended. The token must be the caller's own, already
// checked to be one of the email's live sessions; an empty one is refused,
// since keeping nothing is DeleteForEmail.
func (s *Store) EndOthers(ctx context.Context, email, keepToken string) (int, error) {
	e := normalise(email)
	if e == "" {
		return 0, errors.New("end other sessions: empty email")
	}
	if keepToken == "" {
		return 0, errors.New("end other sessions: no current session")
	}
	var n int64
	err := db.WithTx(ctx, s.db, s.q, func(q *sessiondb.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.DeleteOtherSessionsOfEmail(ctx, sessiondb.DeleteOtherSessionsOfEmailParams{Email: e, KeepHash: hash(keepToken)})
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("end other sessions of %s: %w", e, err)
	}
	return int(n), nil
}

func normalise(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
