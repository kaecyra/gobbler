package apikey

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
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/apikey/apikeydb"
	"github.com/kaecyra/gobbler/internal/db"
)

var (
	// ErrNotFound is returned (wrapped) when no key matches for the caller: it
	// does not exist, belongs to someone else, or the creator is not a member
	// of the household. The cases are one error, so a probe learns nothing.
	ErrNotFound = errors.New("key not found")
	// ErrInvalid is returned (wrapped) for input the store refuses. Its text,
	// from InvalidReason, is written for the person who typed it.
	ErrInvalid = errors.New("invalid key")
)

const (
	// secretBytes is the size of a secret before encoding: 256 bits from a
	// cryptographically secure source.
	secretBytes = 32
	// secretMark starts every secret, so a leaked one is recognisable.
	secretMark = "gob_"
	// prefixLen is how much of the secret the list shows: the mark and eight
	// random characters, 48 bits of 256.
	prefixLen = len(secretMark) + 8
	// MaxNameRunes is the longest key name.
	MaxNameRunes = 60
	// timeLayout is fixed width, so text comparison orders timestamps.
	timeLayout = "2006-01-02T15:04:05.000000000Z"
)

var encoding = base64.RawURLEncoding

// Key is a stored key as the pages list it. It never holds the secret.
type Key struct {
	ID int64
	// Prefix is the non-secret start of the secret.
	Prefix string
	Name   string
	// CreatedBy is the email of the member who made it.
	CreatedBy string
	CreatedAt time.Time
	// ExpiresAt is zero for a key that never expires.
	ExpiresAt time.Time
	// LastUsedAt is zero for a key never used.
	LastUsedAt time.Time
}

// Expired reports whether the key has an expiry that is not after now.
func (k Key) Expired(now time.Time) bool {
	return !k.ExpiresAt.IsZero() && !k.ExpiresAt.After(now)
}

// Store creates, lists and revokes keys. It is safe for concurrent use.
type Store struct {
	db  *sql.DB
	q   *apikeydb.Queries
	now func() time.Time
	// rand is where secrets are drawn from.
	rand io.Reader
}

// New returns a Store over d, which must already be migrated.
func New(d *sql.DB) *Store {
	return &Store{db: d, q: apikeydb.New(d), now: time.Now, rand: rand.Reader}
}

// hash is the only form of a secret the database sees.
func hash(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

func (s *Store) stamp(t time.Time) string { return t.UTC().Format(timeLayout) }

// draw returns a new secret.
func (s *Store) draw() (string, error) {
	raw := make([]byte, secretBytes)
	if _, err := io.ReadFull(s.rand, raw); err != nil {
		return "", fmt.Errorf("draw api key secret: %w", err)
	}
	return secretMark + encoding.EncodeToString(raw), nil
}

// checkName trims the name and refuses a blank or over-long one.
func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", fmt.Errorf("%w: a key needs a name", ErrInvalid)
	case utf8.RuneCountInString(name) > MaxNameRunes:
		return "", fmt.Errorf("%w: a key's name is at most %d characters", ErrInvalid, MaxNameRunes)
	}
	return name, nil
}

// expiry is the expiry column for a key made now that lasts lifetime: NULL for
// zero, which is "no expiry".
func (s *Store) expiry(now time.Time, lifetime time.Duration) (sql.NullString, error) {
	switch {
	case lifetime < 0:
		return sql.NullString{}, fmt.Errorf("%w: a key's lifetime cannot be negative", ErrInvalid)
	case lifetime == 0:
		return sql.NullString{}, nil
	}
	return sql.NullString{String: s.stamp(now.Add(lifetime)), Valid: true}, nil
}

// CreatePersonal makes a key that acts as the person with the email, which
// lives for lifetime (zero: it does not expire). It returns the secret, which
// is not stored and exists nowhere else: the caller shows it once and must
// never log it.
func (s *Store) CreatePersonal(ctx context.Context, email, name string, lifetime time.Duration) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return "", errors.New("create api key: no owner email")
	}
	n, err := checkName(name)
	if err != nil {
		return "", err
	}
	return s.create(ctx, lifetime, func(q *apikeydb.Queries, secret string, created string, expires sql.NullString) error {
		_, err := q.InsertPersonalKey(ctx, apikeydb.InsertPersonalKeyParams{
			TokenHash: hash(secret), Prefix: secret[:prefixLen], Name: n, OwnerEmail: sql.NullString{String: e, Valid: true}, CreatedAt: created, ExpiresAt: expires,
		})
		return err
	})
}

// CreateHousehold makes a key that acts for the household, on behalf of the
// member by. A creator who is not a member of the household gets ErrNotFound.
// The secret is returned as for CreatePersonal.
func (s *Store) CreateHousehold(ctx context.Context, householdID int64, by, name string, lifetime time.Duration) (string, error) {
	e := strings.ToLower(strings.TrimSpace(by))
	if e == "" {
		return "", errors.New("create api key: no creator email")
	}
	n, err := checkName(name)
	if err != nil {
		return "", err
	}
	secret, err := s.create(ctx, lifetime, func(q *apikeydb.Queries, secret string, created string, expires sql.NullString) error {
		_, err := q.InsertHouseholdKey(ctx, apikeydb.InsertHouseholdKeyParams{
			TokenHash: hash(secret), Prefix: secret[:prefixLen], Name: n, HouseholdID: sql.NullInt64{Int64: householdID, Valid: true},
			CreatedBy: e, CreatedAt: created, ExpiresAt: expires,
		})
		return err
	})
	// The insert matched no member row.
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: %s is not a member of household %d", ErrNotFound, e, householdID)
	}
	return secret, err
}

// create draws a secret and runs insert with it in a transaction. Errors it
// wraps never contain the secret.
func (s *Store) create(ctx context.Context, lifetime time.Duration, insert func(q *apikeydb.Queries, secret, created string, expires sql.NullString) error) (string, error) {
	now := s.now()
	expires, err := s.expiry(now, lifetime)
	if err != nil {
		return "", err
	}
	secret, err := s.draw()
	if err != nil {
		return "", err
	}
	err = db.WithTx(ctx, s.db, s.q, func(q *apikeydb.Queries, _ *sql.Tx) error {
		return insert(q, secret, s.stamp(now), expires)
	})
	// A clash on a 256-bit random secret means the random source is broken, so
	// it is reported as the failure it is, not redrawn.
	if err != nil {
		return "", fmt.Errorf("insert api key: %w", err)
	}
	return secret, nil
}

// ListPersonal returns the person's keys, oldest first.
func (s *Store) ListPersonal(ctx context.Context, email string) ([]Key, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	rows, err := s.q.ListPersonalKeys(ctx, sql.NullString{String: e, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list api keys of %s: %w", e, err)
	}
	out := make([]Key, 0, len(rows))
	for _, r := range rows {
		k, err := toKey(r.ID, r.Prefix, r.Name, r.CreatedBy, r.CreatedAt, r.ExpiresAt, r.LastUsedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

// ListHousehold returns the household's keys, oldest first.
func (s *Store) ListHousehold(ctx context.Context, householdID int64) ([]Key, error) {
	rows, err := s.q.ListHouseholdKeys(ctx, sql.NullInt64{Int64: householdID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list api keys of household %d: %w", householdID, err)
	}
	out := make([]Key, 0, len(rows))
	for _, r := range rows {
		k, err := toKey(r.ID, r.Prefix, r.Name, r.CreatedBy, r.CreatedAt, r.ExpiresAt, r.LastUsedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func toKey(id int64, prefix, name, createdBy, created string, expires, used sql.NullString) (Key, error) {
	k := Key{ID: id, Prefix: prefix, Name: name, CreatedBy: createdBy}
	var err error
	if k.CreatedAt, err = time.Parse(timeLayout, created); err != nil {
		return Key{}, fmt.Errorf("parse created time of api key %d: %w", id, err)
	}
	if expires.Valid {
		if k.ExpiresAt, err = time.Parse(timeLayout, expires.String); err != nil {
			return Key{}, fmt.Errorf("parse expiry of api key %d: %w", id, err)
		}
	}
	if used.Valid {
		if k.LastUsedAt, err = time.Parse(timeLayout, used.String); err != nil {
			return Key{}, fmt.Errorf("parse last use of api key %d: %w", id, err)
		}
	}
	return k, nil
}

// RevokePersonal deletes the person's own key. Someone else's key, and one
// that does not exist, are both ErrNotFound.
func (s *Store) RevokePersonal(ctx context.Context, email string, id int64) error {
	e := strings.ToLower(strings.TrimSpace(email))
	return s.revoke(ctx, id, func(q *apikeydb.Queries) (int64, error) {
		return q.RevokePersonalKey(ctx, apikeydb.RevokePersonalKeyParams{ID: id, OwnerEmail: sql.NullString{String: e, Valid: true}})
	})
}

// RevokeHousehold deletes a key of the household. A key of another household,
// a personal key and one that does not exist are all ErrNotFound; that the
// caller is a member of the household is the caller's check (ADR-00018).
func (s *Store) RevokeHousehold(ctx context.Context, householdID, id int64) error {
	return s.revoke(ctx, id, func(q *apikeydb.Queries) (int64, error) {
		return q.RevokeHouseholdKey(ctx, apikeydb.RevokeHouseholdKeyParams{ID: id, HouseholdID: sql.NullInt64{Int64: householdID, Valid: true}})
	})
}

func (s *Store) revoke(ctx context.Context, id int64, del func(q *apikeydb.Queries) (int64, error)) error {
	var n int64
	err := db.WithTx(ctx, s.db, s.q, func(q *apikeydb.Queries, _ *sql.Tx) error {
		var err error
		n, err = del(q)
		return err
	})
	if err != nil {
		return fmt.Errorf("revoke api key %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: key %d", ErrNotFound, id)
	}
	return nil
}

// RevokeForMember is the hook that makes removing a member, or their leaving,
// revoke their credentials at once (ADR-00031): their personal keys and the
// household keys they made. The router registers it with
// household.Store.OnMemberLeave, so it runs inside the transaction that removes
// the member and a failure here rolls the removal back. household's hook type
// is matched by signature, so neither store imports the other.
func (s *Store) RevokeForMember(ctx context.Context, tx *sql.Tx, email string) error {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		// No key has an empty email, so this can only be a caller that lost
		// the address it meant to revoke keys for.
		return errors.New("revoke api keys: empty email")
	}
	if _, err := s.q.WithTx(tx).RevokeMemberKeys(ctx, sql.NullString{String: e, Valid: true}); err != nil {
		return fmt.Errorf("revoke api keys of %s: %w", e, err)
	}
	return nil
}

// InvalidReason returns the message for input the store refused, such as a
// blank name, and whether err is one. Those messages are written by the store
// for the person who typed the input; nothing else is safe to show.
func InvalidReason(err error) (string, bool) {
	if errors.Is(err, ErrInvalid) {
		return err.Error(), true
	}
	return "", false
}

// dateLayout is how the pages show when a key was made, expires or was used.
const dateLayout = "2006-01-02"

// CreatedText is the date the key was made.
func (k Key) CreatedText() string { return k.CreatedAt.UTC().Format(dateLayout) }

// ExpiryText says when the key stops working, as of now.
func (k Key) ExpiryText(now time.Time) string {
	switch {
	case k.ExpiresAt.IsZero():
		return "No expiry"
	case k.Expired(now):
		return "Expired " + k.ExpiresAt.UTC().Format(dateLayout)
	}
	return "Expires " + k.ExpiresAt.UTC().Format(dateLayout)
}

// UsedText says when the key was last used.
func (k Key) UsedText() string {
	if k.LastUsedAt.IsZero() {
		return "Never used"
	}
	return "Last used " + k.LastUsedAt.UTC().Format(dateLayout)
}
