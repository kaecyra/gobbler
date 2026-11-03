package profile

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/profile/profiledb"
)

var (
	// ErrNotFound is returned (wrapped) when no profile has the address or
	// email asked for. A guessed address is this.
	ErrNotFound = errors.New("profile not found")
	// ErrInvalid is returned (wrapped) for input the store refuses. Its text,
	// from InvalidReason, is written for the person who typed it.
	ErrInvalid = errors.New("invalid profile")
)

const (
	// addressBytes is the size of an address before encoding: 128 random bits,
	// which is 22 characters of unpadded base64url (the migration checks it).
	addressBytes = 16
	// MaxNameRunes is the longest display name.
	MaxNameRunes = 60
	// timeLayout is fixed width, so text comparison orders timestamps.
	timeLayout = "2006-01-02T15:04:05.000000000Z"
)

var addressEncoding = base64.RawURLEncoding

// Profile is a person's stored profile.
type Profile struct {
	// Email is the key, lower-cased.
	Email string
	// Address is the opaque random name of the profile page.
	Address string
	// DisplayName is what the person set, empty if nothing; use Name.
	DisplayName string
	// Avatar is the reference to an uploaded picture, empty until one is.
	Avatar    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Name is the display name, or the email's local part when none is set
// (ADR-00032).
func (p Profile) Name() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	local, _, _ := strings.Cut(p.Email, "@")
	return local
}

// Store reads and writes profiles. It is safe for concurrent use.
type Store struct {
	db   *sql.DB
	q    *profiledb.Queries
	now  func() time.Time
	rand io.Reader
}

// New returns a Store over d, which must already be migrated.
func New(d *sql.DB) *Store {
	return &Store{db: d, q: profiledb.New(d), now: time.Now, rand: rand.Reader}
}

func normalise(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if e == "" {
		return "", fmt.Errorf("%w: a profile needs an email", ErrInvalid)
	}
	return e, nil
}

// Ensure returns the profile of the email, creating it with a new random
// address the first time. Calling it again returns the same address.
func (s *Store) Ensure(ctx context.Context, email string) (Profile, error) {
	e, err := normalise(email)
	if err != nil {
		return Profile{}, err
	}
	row, err := s.q.GetProfileByEmail(ctx, e)
	if err == nil {
		return convert(row)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Profile{}, fmt.Errorf("read profile of %s: %w", e, err)
	}
	raw := make([]byte, addressBytes)
	if _, err := io.ReadFull(s.rand, raw); err != nil {
		return Profile{}, fmt.Errorf("draw profile address: %w", err)
	}
	stamp := s.now().UTC().Format(timeLayout)
	err = db.WithTx(ctx, s.db, s.q, func(q *profiledb.Queries, _ *sql.Tx) error {
		// ON CONFLICT (email) keeps a profile another request created first. A
		// clash of the 128-bit address would fail the unique constraint, which
		// means the random source is broken, so it is reported, not redrawn.
		return q.InsertProfile(ctx, profiledb.InsertProfileParams{
			Email: e, Address: addressEncoding.EncodeToString(raw), CreatedAt: stamp, UpdatedAt: stamp,
		})
	})
	if err != nil {
		return Profile{}, fmt.Errorf("create profile of %s: %w", e, err)
	}
	row, err = s.q.GetProfileByEmail(ctx, e)
	if err != nil {
		return Profile{}, fmt.Errorf("read created profile of %s: %w", e, err)
	}
	return convert(row)
}

// ByAddress returns the profile a page address names, or ErrNotFound. An
// address of the wrong shape cannot name one and is ErrNotFound as well.
func (s *Store) ByAddress(ctx context.Context, address string) (Profile, error) {
	if b, err := addressEncoding.DecodeString(address); err != nil || len(b) != addressBytes {
		return Profile{}, fmt.Errorf("profile address: %w", ErrNotFound)
	}
	row, err := s.q.GetProfileByAddress(ctx, address)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, fmt.Errorf("profile address: %w", ErrNotFound)
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read profile by address: %w", err)
	}
	return convert(row)
}

// ByEmail returns the profile of the email, or ErrNotFound when the person has
// not opened theirs yet. Unlike Ensure it never writes, so a page that only
// wants the avatar can ask on every request.
func (s *Store) ByEmail(ctx context.Context, email string) (Profile, error) {
	e, err := normalise(email)
	if err != nil {
		return Profile{}, err
	}
	row, err := s.q.GetProfileByEmail(ctx, e)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, fmt.Errorf("profile of %s: %w", e, ErrNotFound)
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read profile of %s: %w", e, err)
	}
	return convert(row)
}

// Rename sets the display name of the email's profile. The name is trimmed;
// an empty one clears it, so the page shows the email's local part again. A
// name longer than MaxNameRunes or holding a control character is ErrInvalid.
func (s *Store) Rename(ctx context.Context, email, name string) error {
	e, err := normalise(email)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	switch {
	case !utf8.ValidString(name):
		return fmt.Errorf("%w: the name is not valid text", ErrInvalid)
	case utf8.RuneCountInString(name) > MaxNameRunes:
		return fmt.Errorf("%w: the name can be at most %d characters", ErrInvalid, MaxNameRunes)
	case strings.ContainsFunc(name, unicode.IsControl):
		return fmt.Errorf("%w: the name cannot contain control characters", ErrInvalid)
	}
	err = db.WithTx(ctx, s.db, s.q, func(q *profiledb.Queries, _ *sql.Tx) error {
		n, err := q.SetDisplayName(ctx, profiledb.SetDisplayNameParams{
			DisplayName: name, Now: s.now().UTC().Format(timeLayout), Email: e,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("profile of %s: %w", e, ErrNotFound)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("rename profile: %w", err)
	}
	return nil
}

// InvalidReason returns the message for input the store refused, such as a
// name that is too long, and whether err is one. Those messages are written by
// the store for the person who typed the input; nothing else is safe to show.
func InvalidReason(err error) (string, bool) {
	if errors.Is(err, ErrInvalid) {
		return err.Error(), true
	}
	return "", false
}

func convert(row profiledb.Profile) (Profile, error) {
	created, err := time.Parse(timeLayout, row.CreatedAt)
	if err != nil {
		return Profile{}, fmt.Errorf("profile %s: created_at: %w", row.Email, err)
	}
	updated, err := time.Parse(timeLayout, row.UpdatedAt)
	if err != nil {
		return Profile{}, fmt.Errorf("profile %s: updated_at: %w", row.Email, err)
	}
	return Profile{
		Email: row.Email, Address: row.Address, DisplayName: row.DisplayName, Avatar: row.Avatar,
		CreatedAt: created, UpdatedAt: updated,
	}, nil
}
