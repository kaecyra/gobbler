package household

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household/householddb"
)

// Sentinel errors for the member actions.
var (
	// ErrLastMember is returned when the only member of a household tries to
	// leave it. They must delete the household instead (ADR-00018).
	ErrLastMember = errors.New("you are the last member; delete the household instead of leaving it")
	// ErrNotLastMember is returned when a member tries to delete a household
	// that has other members. Only the last member can.
	ErrNotLastMember = errors.New("only the last member can delete the household")
	// ErrConfirmation is returned when the name typed to confirm a deletion is
	// not the household's name.
	ErrConfirmation = errors.New("the name typed does not match the household's name")
)

// Invitation is an invitation as the pages show it.
type Invitation struct {
	ID   int64
	Kind Kind
	// Email is the address the invitation is for.
	Email    string
	IssuedBy string
	// HouseholdID and HouseholdName are those of a join invitation's household;
	// both are zero for a household invitation. HouseholdName is filled only by
	// InvitationFor.
	HouseholdID   int64
	HouseholdName string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

func toInvitation(id int64, kind, email string, household sql.NullInt64, issuedBy, created, expires string) (Invitation, error) {
	c, err := parseTime(created)
	if err != nil {
		return Invitation{}, err
	}
	e, err := parseTime(expires)
	if err != nil {
		return Invitation{}, err
	}
	return Invitation{ID: id, Kind: Kind(kind), Email: email, HouseholdID: household.Int64, IssuedBy: issuedBy, CreatedAt: c, ExpiresAt: e}, nil
}

// PendingForHousehold returns the join invitations into the household that can
// still be accepted, oldest first.
func (s *Store) PendingForHousehold(ctx context.Context, householdID int64) ([]Invitation, error) {
	rows, err := s.q.ListPendingJoinInvitations(ctx, householddb.ListPendingJoinInvitationsParams{
		HouseholdID: sql.NullInt64{Int64: householdID, Valid: true}, Now: s.stamp(),
	})
	if err != nil {
		return nil, fmt.Errorf("list invitations of household %d: %w", householdID, err)
	}
	out := make([]Invitation, 0, len(rows))
	for _, r := range rows {
		inv, err := toInvitation(r.ID, r.Kind, r.Email, r.HouseholdID, r.IssuedBy, r.CreatedAt, r.ExpiresAt)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

// PendingHouseholdInvitations returns the household invitations issuer has
// issued that can still be accepted, oldest first. They name no household and
// say nothing about any.
func (s *Store) PendingHouseholdInvitations(ctx context.Context, issuer string) ([]Invitation, error) {
	e, err := NormalizeEmail(issuer)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListPendingHouseholdInvitations(ctx, householddb.ListPendingHouseholdInvitationsParams{
		IssuedBy: e, Now: s.stamp(),
	})
	if err != nil {
		return nil, fmt.Errorf("list household invitations of %s: %w", e, err)
	}
	out := make([]Invitation, 0, len(rows))
	for _, r := range rows {
		inv, err := toInvitation(r.ID, r.Kind, r.Email, r.HouseholdID, r.IssuedBy, r.CreatedAt, r.ExpiresAt)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

// InvitationFor returns invitation id for the verified email it is addressed
// to. A missing invitation and one addressed to someone else are both
// ErrNotFound; one that can no longer be accepted is ErrUsed, ErrRevoked or
// ErrExpired, as Accept would say.
func (s *Store) InvitationFor(ctx context.Context, id int64, email string) (Invitation, error) {
	e, err := NormalizeEmail(email)
	if err != nil {
		return Invitation{}, err
	}
	r, err := s.q.GetInvitationFor(ctx, householddb.GetInvitationForParams{ID: id, Email: e})
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, fmt.Errorf("%w: invitation %d", ErrNotFound, id)
	}
	if err != nil {
		return Invitation{}, fmt.Errorf("load invitation %d: %w", id, err)
	}
	inv, err := toInvitation(r.ID, r.Kind, r.Email, r.HouseholdID, r.IssuedBy, r.CreatedAt, r.ExpiresAt)
	if err != nil {
		return Invitation{}, err
	}
	inv.HouseholdName = r.HouseholdName
	switch {
	case r.AcceptedAt.Valid:
		return Invitation{}, fmt.Errorf("invitation %d: %w", id, ErrUsed)
	case r.RevokedAt.Valid:
		return Invitation{}, fmt.Errorf("invitation %d: %w", id, ErrRevoked)
	case !inv.ExpiresAt.After(s.now()):
		return Invitation{}, fmt.Errorf("invitation %d: %w", id, ErrExpired)
	}
	return inv, nil
}

// Remove takes target out of the household on behalf of by, who must be
// another member of it. A target that is not in the household, and a remover
// who is not, are both ErrNotFound, so the answer says nothing about other
// households; removing oneself is ErrInvalid (that is Leave).
func (s *Store) Remove(ctx context.Context, householdID int64, by, target string) error {
	remover, err := NormalizeEmail(by)
	if err != nil {
		return err
	}
	t, err := NormalizeEmail(target)
	if err != nil {
		return err
	}
	if remover == t {
		return fmt.Errorf("%w: use leave to remove yourself", ErrInvalid)
	}
	err = db.WithTx(ctx, s.db, s.q, func(q *householddb.Queries, _ *sql.Tx) error {
		n, err := q.RemoveMember(ctx, householddb.RemoveMemberParams{HouseholdID: householdID, Target: t, RemovedBy: remover})
		if err != nil {
			return fmt.Errorf("remove %s from household %d: %w", t, householdID, err)
		}
		if n == 0 {
			return fmt.Errorf("%w: %s is not in household %d", ErrNotFound, t, householdID)
		}
		return s.revokeIssued(ctx, q, t, householdID)
	})
	return err
}

// revokeIssued withdraws the open join invitations issuer made into the
// household, for a member who is no longer in it.
func (s *Store) revokeIssued(ctx context.Context, q *householddb.Queries, issuer string, householdID int64) error {
	err := q.RevokeMemberInvitations(ctx, householddb.RevokeMemberInvitationsParams{
		Now: sql.NullString{String: s.stamp(), Valid: true}, IssuedBy: issuer, HouseholdID: sql.NullInt64{Int64: householdID, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("revoke invitations of %s into household %d: %w", issuer, householdID, err)
	}
	return nil
}

// Leave takes the email out of its household and withdraws the invitations it
// issued into it. The household's last member is refused with ErrLastMember;
// someone in no household gets ErrNotFound.
func (s *Store) Leave(ctx context.Context, email string) error {
	e, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	left := true
	err = db.WithTx(ctx, s.db, s.q, func(q *householddb.Queries, _ *sql.Tx) error {
		hid, err := q.LeaveHousehold(ctx, e)
		if errors.Is(err, sql.ErrNoRows) {
			left = false
			return nil
		}
		if err != nil {
			return fmt.Errorf("leave household as %s: %w", e, err)
		}
		return s.revokeIssued(ctx, q, e, hid)
	})
	if err != nil || left {
		return err
	}
	if _, err := lookup(ctx, s.q, e); err != nil {
		return err
	}
	return fmt.Errorf("%s: %w", e, ErrLastMember)
}

// Delete removes the household and, through the schema's cascades, everything
// it holds. by must be a member and the household's only one, and confirm must
// be the household's name (ADR-00018). The checks and the delete are one
// transaction, so a member joining meanwhile cannot lose a household they just
// entered.
func (s *Store) Delete(ctx context.Context, householdID int64, by, confirm string) error {
	e, err := NormalizeEmail(by)
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.db, s.q, func(q *householddb.Queries, _ *sql.Tx) error {
		n, err := q.TouchHousehold(ctx, householdID)
		if err != nil {
			return fmt.Errorf("lock household %d: %w", householdID, err)
		}
		mine, err := lookup(ctx, q, e)
		if n == 0 || errors.Is(err, ErrNotFound) || (err == nil && mine.ID != householdID) {
			return fmt.Errorf("%w: %s is not a member of household %d", ErrNotFound, e, householdID)
		}
		if err != nil {
			return err
		}
		members, err := q.CountMembers(ctx, householdID)
		if err != nil {
			return fmt.Errorf("count members of household %d: %w", householdID, err)
		}
		if members > 1 {
			return fmt.Errorf("household %d: %w", householdID, ErrNotLastMember)
		}
		if strings.TrimSpace(confirm) != mine.Name {
			return fmt.Errorf("household %d: %w", householdID, ErrConfirmation)
		}
		if _, err := q.DeleteHousehold(ctx, householdID); err != nil {
			return fmt.Errorf("delete household %d: %w", householdID, err)
		}
		return nil
	})
}

// InvalidReason returns the message of an error caused by input the store
// refused, such as a malformed email, and whether err is one. Those messages
// are written by the store for the person who typed the input; nothing else
// is safe to show.
func InvalidReason(err error) (string, bool) {
	if errors.Is(err, ErrInvalid) {
		return err.Error(), true
	}
	return "", false
}
