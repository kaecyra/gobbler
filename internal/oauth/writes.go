package oauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kaecyra/gobbler/internal/oauth/oauthdb"
)

// Write is one row of a grant's write log.
type Write struct {
	GrantID     int64
	HouseholdID int64
	Tool        string
	Target      string
	At          time.Time
}

// RecordWrite logs that the grant p made a write through tool, on target, for
// the household (ADR-00031 obligation 5, applied to tokens by ADR-00034). p is
// the Principal Verify returned, so the row carries the grant as it was when the
// request was authenticated: a grant revoked while the request ran is still
// logged against itself, and Writes matches the snapshot. The log outlives the
// grant: revoking it deletes the grant and its tokens, not this record.
func (s *Store) RecordWrite(ctx context.Context, p Principal, householdID int64, tool, target string) error {
	if tool == "" || p.GrantID == 0 || p.ClientID == "" || p.Email == "" || p.GrantCreatedAt.IsZero() || householdID == 0 {
		return errors.New("record oauth grant write: a verified grant, a household and a tool are required")
	}
	err := s.q.InsertGrantWrite(ctx, oauthdb.InsertGrantWriteParams{
		GrantID: p.GrantID, GrantClientID: p.ClientID, GrantOwner: p.Email, GrantCreatedAt: stamp(p.GrantCreatedAt),
		HouseholdID: householdID, Tool: tool, Target: target, CreatedAt: stamp(s.now()),
	})
	if err != nil {
		return fmt.Errorf("record write of oauth grant %d: %w", p.GrantID, err)
	}
	return nil
}

// Writes returns what the grant p names has written, oldest first. It matches
// the grant's client, owner and creation time as well as its id, because SQLite
// gives a revoked grant's id to a later grant and that grant did not write the
// old one's rows.
func (s *Store) Writes(ctx context.Context, p Principal) ([]Write, error) {
	rows, err := s.q.ListGrantWrites(ctx, oauthdb.ListGrantWritesParams{
		GrantID: p.GrantID, ClientID: p.ClientID, Owner: p.Email, GrantCreatedAt: stamp(p.GrantCreatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("list writes of oauth grant %d: %w", p.GrantID, err)
	}
	out := make([]Write, 0, len(rows))
	for _, r := range rows {
		at, err := time.Parse(timeLayout, r.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse time of grant write %d: %w", r.ID, err)
		}
		out = append(out, Write{GrantID: r.GrantID, HouseholdID: r.HouseholdID, Tool: r.Tool, Target: r.Target, At: at})
	}
	return out, nil
}
