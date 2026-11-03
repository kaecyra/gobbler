package apikey

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/apikey/apikeydb"
)

// ErrUnknownKey is returned (wrapped) by Lookup for a secret that names no
// live key: never issued, revoked or expired. The cases are one error, so a
// caller cannot tell a probe which one it hit.
var ErrUnknownKey = errors.New("unknown, revoked or expired api key")

// touchEvery is the least time between two stamps of a key's last use.
const touchEvery = time.Minute

// Principal is who a live key acts as (ADR-00031). Exactly one of Email and
// HouseholdID is set.
type Principal struct {
	// KeyID identifies the key in the write log; it is not secret. Ids are
	// reused after a revocation, so the log is read with the whole Principal
	// (Writes), whose Prefix and CreatedAt tell two keys of one id apart.
	KeyID     int64
	Prefix    string
	CreatedAt time.Time
	// Email is the owner of a personal key.
	Email string
	// HouseholdID and HouseholdName are the household a household key acts
	// for.
	HouseholdID   int64
	HouseholdName string
}

// Lookup finds the live key a secret names and stamps its last use. A secret
// that matches no key, or whose key has expired, is ErrUnknownKey. The secret
// is never part of an error.
func (s *Store) Lookup(ctx context.Context, secret string) (Principal, error) {
	row, err := s.q.FindKeyByHash(ctx, hash(secret))
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrUnknownKey
	}
	if err != nil {
		return Principal{}, fmt.Errorf("look up api key: %w", err)
	}
	now := s.now()
	if row.ExpiresAt.Valid {
		exp, err := time.Parse(timeLayout, row.ExpiresAt.String)
		if err != nil {
			return Principal{}, fmt.Errorf("parse expiry of api key %d: %w", row.ID, err)
		}
		if !exp.After(now) {
			return Principal{}, ErrUnknownKey
		}
	}
	created, err := time.Parse(timeLayout, row.CreatedAt)
	if err != nil {
		return Principal{}, fmt.Errorf("parse creation time of api key %d: %w", row.ID, err)
	}
	p := Principal{KeyID: row.ID, Prefix: row.Prefix, CreatedAt: created}
	switch {
	case row.OwnerEmail.Valid:
		p.Email = row.OwnerEmail.String
	case row.HouseholdID.Valid:
		p.HouseholdID, p.HouseholdName = row.HouseholdID.Int64, row.HouseholdName.String
	default:
		// The table's CHECK makes this impossible; a row that breaks it is a
		// damaged database, not a refusal.
		return Principal{}, fmt.Errorf("api key %d has neither an owner nor a household", row.ID)
	}
	if err := s.q.TouchKey(ctx, apikeydb.TouchKeyParams{
		Now: sql.NullString{String: s.stamp(now), Valid: true}, ID: row.ID, OlderThan: sql.NullString{String: s.stamp(now.Add(-touchEvery)), Valid: true},
	}); err != nil {
		// Fail loud: a database that cannot take this write is broken, and the
		// key list would otherwise lie about use.
		return Principal{}, fmt.Errorf("stamp last use of api key %d: %w", row.ID, err)
	}
	return p, nil
}

// Write is one row of the write log.
type Write struct {
	KeyID       int64
	HouseholdID int64
	Tool        string
	Target      string
	At          time.Time
}

// RecordWrite logs that the key p made a write through tool, on target, for the
// household (ADR-00031 obligation 5). p is the Principal Lookup returned, so the
// row carries the key as it was when the request was authenticated: a key
// revoked while the request ran is still logged against itself, and Writes
// matches the snapshot.
func (s *Store) RecordWrite(ctx context.Context, p Principal, householdID int64, tool, target string) error {
	if tool == "" || p.KeyID == 0 || p.Prefix == "" || p.CreatedAt.IsZero() || (p.Email == "" && p.HouseholdID == 0) || householdID == 0 {
		return errors.New("record api key write: a looked-up key, a household and a tool are required")
	}
	err := s.q.InsertKeyWrite(ctx, apikeydb.InsertKeyWriteParams{
		KeyID: p.KeyID, KeyPrefix: p.Prefix, KeyOwner: p.owner(), KeyCreatedAt: s.stamp(p.CreatedAt),
		HouseholdID: householdID, Tool: tool, Target: target, CreatedAt: s.stamp(s.now()),
	})
	if err != nil {
		return fmt.Errorf("record write of api key %d: %w", p.KeyID, err)
	}
	return nil
}

// owner is how the write log names the key's owner: the email of a personal
// key, or "household:<id>". It is the one place that builds the name, for
// RecordWrite and Writes alike.
func (p Principal) owner() string {
	if p.Email != "" {
		return p.Email
	}
	return fmt.Sprintf("household:%d", p.HouseholdID)
}

// Writes returns what the key p names has written, oldest first. It matches the
// key's prefix, owner and creation time as well as its id, because SQLite gives
// a revoked key's id to a later key and that key did not write the old one's
// rows.
func (s *Store) Writes(ctx context.Context, p Principal) ([]Write, error) {
	rows, err := s.q.ListKeyWrites(ctx, apikeydb.ListKeyWritesParams{
		KeyID: p.KeyID, KeyPrefix: p.Prefix, KeyOwner: p.owner(), KeyCreatedAt: s.stamp(p.CreatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("list writes of api key %d: %w", p.KeyID, err)
	}
	out := make([]Write, 0, len(rows))
	for _, r := range rows {
		at, err := time.Parse(timeLayout, r.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse time of write %d: %w", r.ID, err)
		}
		out = append(out, Write{KeyID: r.KeyID, HouseholdID: r.HouseholdID, Tool: r.Tool, Target: r.Target, At: at})
	}
	return out, nil
}
