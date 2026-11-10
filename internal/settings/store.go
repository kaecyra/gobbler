package settings

import (
	"context"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/settings/settingsdb"
)

var (
	// ErrUnset is returned (wrapped) when no value is stored for a field.
	ErrUnset = errors.New("setting not set")
	// ErrUnreadable is returned (wrapped) when a secret is stored but cannot
	// be opened: no encryption key is configured, or the key does not match
	// the one it was sealed under. A caller treats the secret as unset.
	ErrUnreadable = errors.New("secret unreadable")
	// ErrNoKey is returned (wrapped) when a secret is saved with no
	// encryption key configured.
	ErrNoKey = errors.New("no encryption key configured")
	// ErrInvalid is returned (wrapped) for input the store refuses.
	ErrInvalid = errors.New("invalid setting")
)

// MaxValueBytes is the longest value the store keeps.
const MaxValueBytes = 4096

// Field addresses one setting.
type Field struct {
	Integration string
	Name        string
}

// State is what is known about a secret without revealing it.
type State int

const (
	// StateUnset means nothing is stored.
	StateUnset State = iota
	// StateSet means a value is stored and opens under the current key.
	StateSet
	// StateUnreadable means a value is stored and cannot be opened.
	StateUnreadable
)

// Store keeps integration settings. Create it with New.
type Store struct {
	db   *sql.DB
	q    *settingsdb.Queries
	aead cipher.AEAD // nil when no usable encryption key is configured
	now  func() time.Time
	rand io.Reader
}

// New returns a store over d. envKey is the decoded encryption key from
// ParseKey; nil means none is configured, and then secrets cannot be saved or
// opened.
func New(d *sql.DB, envKey []byte) (*Store, error) {
	s := &Store{db: d, q: settingsdb.New(d), now: time.Now, rand: rand.Reader}
	if envKey != nil {
		a, err := newAEAD(envKey)
		if err != nil {
			return nil, err
		}
		s.aead = a
	}
	return s, nil
}

// HasKey reports whether an encryption key is configured.
func (s *Store) HasKey() bool { return s.aead != nil }

func check(f Field, v string) error {
	if f.Integration == "" || f.Name == "" {
		return fmt.Errorf("%w: an integration and a field name are required", ErrInvalid)
	}
	if v == "" {
		return fmt.Errorf("%w: %s %s is empty", ErrInvalid, f.Integration, f.Name)
	}
	if len(v) > MaxValueBytes {
		return fmt.Errorf("%w: %s %s is longer than %d bytes", ErrInvalid, f.Integration, f.Name, MaxValueBytes)
	}
	return nil
}

// SetValue stores a plain setting.
func (s *Store) SetValue(ctx context.Context, f Field, v string) error {
	if err := check(f, v); err != nil {
		return err
	}
	return s.put(ctx, f, 0, []byte(v))
}

// Value returns a plain setting, or ErrUnset. A field holding a secret is not
// a plain value: Value returns an error rather than its ciphertext.
func (s *Store) Value(ctx context.Context, f Field) (string, error) {
	secret, v, err := s.get(ctx, f)
	if err != nil {
		return "", err
	}
	if secret {
		return "", fmt.Errorf("read %s %s: stored as a secret", f.Integration, f.Name)
	}
	return string(v), nil
}

// SetSecret seals and stores a secret. It is refused with ErrNoKey when no
// encryption key is configured.
func (s *Store) SetSecret(ctx context.Context, f Field, v string) error {
	if err := check(f, v); err != nil {
		return err
	}
	if s.aead == nil {
		return fmt.Errorf("save %s %s: %w: set GOBBLER_ENCRYPTION_KEY", f.Integration, f.Name, ErrNoKey)
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(s.rand, nonce); err != nil {
		return fmt.Errorf("draw nonce: %w", err)
	}
	return s.put(ctx, f, 1, s.aead.Seal(nonce, nonce, []byte(v), aad(f)))
}

// Secret returns a secret's value. It returns ErrUnset when nothing is stored
// and ErrUnreadable when a value is stored that cannot be opened.
func (s *Store) Secret(ctx context.Context, f Field) (string, error) {
	secret, v, err := s.get(ctx, f)
	if err != nil {
		return "", err
	}
	if !secret {
		return "", fmt.Errorf("read %s %s: stored as a plain value", f.Integration, f.Name)
	}
	return s.open(f, v)
}

// SecretState reports whether a secret is set, unset or unreadable, for the
// Deployment page. It never returns the value.
func (s *Store) SecretState(ctx context.Context, f Field) (State, error) {
	_, err := s.Secret(ctx, f)
	switch {
	case err == nil:
		return StateSet, nil
	case errors.Is(err, ErrUnset):
		return StateUnset, nil
	case errors.Is(err, ErrUnreadable):
		return StateUnreadable, nil
	}
	return StateUnset, err
}

// Clear removes a setting, plain or secret. Clearing what is not stored is not
// an error.
func (s *Store) Clear(ctx context.Context, f Field) error {
	if f.Integration == "" || f.Name == "" {
		return fmt.Errorf("%w: an integration and a field name are required", ErrInvalid)
	}
	return db.WithTx(ctx, s.db, s.q, func(q *settingsdb.Queries, _ *sql.Tx) error {
		if err := q.DeleteSetting(ctx, settingsdb.DeleteSettingParams{Integration: f.Integration, Field: f.Name}); err != nil {
			return fmt.Errorf("clear %s %s: %w", f.Integration, f.Name, err)
		}
		return nil
	})
}

// LogUnreadable logs, at error level, every stored secret that cannot be
// opened, and returns how many there were. Startup calls it so a missing or
// wrong key is seen in the log (ADR-00057). It logs names, never values.
func (s *Store) LogUnreadable(ctx context.Context, log *slog.Logger) (int, error) {
	rows, err := s.q.ListSecrets(ctx)
	if err != nil {
		return 0, fmt.Errorf("list stored secrets: %w", err)
	}
	n := 0
	for _, r := range rows {
		f := Field{Integration: r.Integration, Name: r.Field}
		if _, err := s.open(f, r.Value); err != nil {
			n++
			log.ErrorContext(ctx, "stored secret cannot be opened; the integration is off until it is entered again",
				"integration", f.Integration, "field", f.Name, "key_configured", s.HasKey())
		}
	}
	return n, nil
}

func (s *Store) open(f Field, sealed []byte) (string, error) {
	if s.aead == nil {
		return "", fmt.Errorf("read %s %s: %w: no encryption key configured", f.Integration, f.Name, ErrUnreadable)
	}
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return "", fmt.Errorf("read %s %s: %w: stored value is too short", f.Integration, f.Name, ErrUnreadable)
	}
	v, err := s.aead.Open(nil, sealed[:n], sealed[n:], aad(f))
	if err != nil {
		return "", fmt.Errorf("read %s %s: %w: wrong key or moved value", f.Integration, f.Name, ErrUnreadable)
	}
	return string(v), nil
}

func (s *Store) get(ctx context.Context, f Field) (secret bool, v []byte, err error) {
	row, err := s.q.GetSetting(ctx, settingsdb.GetSettingParams{Integration: f.Integration, Field: f.Name})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil, fmt.Errorf("read %s %s: %w", f.Integration, f.Name, ErrUnset)
	}
	if err != nil {
		return false, nil, fmt.Errorf("read %s %s: %w", f.Integration, f.Name, err)
	}
	return row.Secret == 1, row.Value, nil
}

func (s *Store) put(ctx context.Context, f Field, secret int64, v []byte) error {
	return db.WithTx(ctx, s.db, s.q, func(q *settingsdb.Queries, _ *sql.Tx) error {
		err := q.PutSetting(ctx, settingsdb.PutSettingParams{
			Integration: f.Integration, Field: f.Name, Secret: secret, Value: v,
			UpdatedAt: s.now().UTC().Format(time.RFC3339),
		})
		if err != nil {
			return fmt.Errorf("save %s %s: %w", f.Integration, f.Name, err)
		}
		return nil
	})
}
