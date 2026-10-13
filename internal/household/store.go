package household

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household/householddb"
)

// Sentinel errors returned (wrapped) by the Store.
var (
	// ErrNotFound is returned when no household has the member, or when an
	// invitation does not exist or is not addressed to the caller. The two
	// invitation cases are one error so a stranger cannot probe for ids.
	ErrNotFound = errors.New("not found")
	// ErrInvalid is returned for input the store refuses, such as a blank
	// household name or a malformed email.
	ErrInvalid = errors.New("invalid input")
	// ErrAlreadyMember is returned when the email already belongs to a
	// household. The person must leave it first (ADR-00018).
	ErrAlreadyMember = errors.New("already a member of a household; leave the current household first")
	// ErrExpired is returned when an invitation is past its expiry.
	ErrExpired = errors.New("invitation has expired")
	// ErrUsed is returned when an invitation has already been accepted.
	ErrUsed = errors.New("invitation has already been used")
	// ErrRevoked is returned when the issuer revoked the invitation.
	ErrRevoked = errors.New("invitation was revoked")
)

// DefaultInvitationTTL is how long an invitation can be accepted when the
// issuer names no other.
const DefaultInvitationTTL = 7 * 24 * time.Hour

// Fixed width, so text comparison in SQL orders timestamps correctly.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// Household is a tenant.
type Household struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// Kind says what accepting an invitation does.
type Kind string

// The invitation kinds (ADR-00018).
const (
	// KindJoin adds the invitee to an existing household.
	KindJoin Kind = "join"
	// KindHousehold creates a household with the invitee as its first member.
	KindHousehold Kind = "household"
)

// Invite describes an invitation to issue.
type Invite struct {
	Kind Kind
	// Email is the address the invitation is for; only a request verified as
	// this email can accept it.
	Email string
	// HouseholdID is the household a join invitation adds to; it must be zero
	// for a household invitation.
	HouseholdID int64
	// IssuedBy is the verified email of the issuer, who alone can revoke it.
	// For a join invitation it must be a member of HouseholdID. That a
	// household invitation's issuer is an operator is the caller's check:
	// operators live in configuration, not here.
	IssuedBy string
	// TTL is how long the invitation stays acceptable; zero means
	// DefaultInvitationTTL.
	TTL time.Duration
}

// Store reads and writes households, members and invitations. It is safe for
// concurrent use.
type Store struct {
	db  *sql.DB
	q   *householddb.Queries
	now func() time.Time
}

// New returns a Store over d, which must already be migrated.
func New(d *sql.DB) *Store {
	return &Store{db: d, q: householddb.New(d), now: time.Now}
}

// NormalizeEmail trims and lower-cases an email and checks it is a bare
// address, the form every table stores.
func NormalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || a.Name != "" {
		return "", fmt.Errorf("%w: %q is not an email address", ErrInvalid, email)
	}
	return e, nil
}

func (s *Store) stamp() string { return s.now().UTC().Format(timeLayout) }

func parseTime(v string) (time.Time, error) {
	t, err := time.Parse(timeLayout, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time %q: %w", v, err)
	}
	return t, nil
}

func toHousehold(r householddb.Household) (Household, error) {
	created, err := parseTime(r.CreatedAt)
	if err != nil {
		return Household{}, err
	}
	return Household{ID: r.ID, Name: r.Name, CreatedAt: created}, nil
}

// ForEmail returns the household the email is a member of, or ErrNotFound. A
// request's household comes from here and from nowhere else (ADR-00018).
func (s *Store) ForEmail(ctx context.Context, email string) (Household, error) {
	e, err := NormalizeEmail(email)
	if err != nil {
		return Household{}, err
	}
	return lookup(ctx, s.q, e)
}

// lookup is ForEmail for an already normalised email, on any queries handle.
func lookup(ctx context.Context, q *householddb.Queries, email string) (Household, error) {
	row, err := q.GetHouseholdByMemberEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return Household{}, fmt.Errorf("%w: no household for %s", ErrNotFound, email)
	}
	if err != nil {
		return Household{}, fmt.Errorf("find household of %s: %w", email, err)
	}
	return toHousehold(row)
}

// Members returns the emails of the household's members, oldest first.
func (s *Store) Members(ctx context.Context, householdID int64) ([]string, error) {
	emails, err := s.q.ListMemberEmails(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("list members of household %d: %w", householdID, err)
	}
	return emails, nil
}

// Create starts a household with first as its only member. A person who
// already belongs to a household gets ErrAlreadyMember and nothing is created.
// Reaching a household through an invitation is Accept; Create is for
// seeding and for tests.
func (s *Store) Create(ctx context.Context, name, first string) (Household, error) {
	var h Household
	err := db.WithTx(ctx, s.db, s.q, func(q *householddb.Queries, _ *sql.Tx) (err error) {
		h, err = s.create(ctx, q, name, first)
		return err
	})
	return h, err
}

func (s *Store) create(ctx context.Context, q *householddb.Queries, name, first string) (Household, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Household{}, fmt.Errorf("%w: a household needs a name", ErrInvalid)
	}
	e, err := NormalizeEmail(first)
	if err != nil {
		return Household{}, err
	}
	stamp := s.stamp()
	id, err := q.InsertHousehold(ctx, householddb.InsertHouseholdParams{Name: name, CreatedAt: stamp})
	if err != nil {
		return Household{}, fmt.Errorf("insert household: %w", err)
	}
	if err := s.addMember(ctx, q, id, e, stamp); err != nil {
		return Household{}, err
	}
	created, err := parseTime(stamp)
	if err != nil {
		return Household{}, err
	}
	return Household{ID: id, Name: name, CreatedAt: created}, nil
}

// addMember relies on the schema's UNIQUE (email) for one household per
// person, so a concurrent pair of joins cannot both succeed.
func (s *Store) addMember(ctx context.Context, q *householddb.Queries, householdID int64, email, stamp string) error {
	err := q.InsertMember(ctx, householddb.InsertMemberParams{HouseholdID: householdID, Email: email, JoinedAt: stamp})
	return db.WrapWrite("add member "+email, err, ErrAlreadyMember, db.ConstraintUnique)
}

// Invite issues an invitation and returns its id.
func (s *Store) Invite(ctx context.Context, in Invite) (int64, error) {
	e, err := NormalizeEmail(in.Email)
	if err != nil {
		return 0, err
	}
	issuer, err := NormalizeEmail(in.IssuedBy)
	if err != nil {
		return 0, err
	}
	ttl := in.TTL
	switch {
	case ttl < 0:
		return 0, fmt.Errorf("%w: invitation lifetime %s is negative", ErrInvalid, ttl)
	case ttl == 0:
		ttl = DefaultInvitationTTL
	}
	var household sql.NullInt64
	switch in.Kind {
	case KindJoin:
		if in.HouseholdID == 0 {
			return 0, fmt.Errorf("%w: a join invitation names a household", ErrInvalid)
		}
		mine, err := lookup(ctx, s.q, issuer)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return 0, err
		}
		if err != nil || mine.ID != in.HouseholdID {
			// Only a member can invite into a household; say so without
			// revealing whether the household exists.
			return 0, fmt.Errorf("%w: %s is not a member of household %d", ErrNotFound, issuer, in.HouseholdID)
		}
		household = sql.NullInt64{Int64: in.HouseholdID, Valid: true}
	case KindHousehold:
		if in.HouseholdID != 0 {
			return 0, fmt.Errorf("%w: a household invitation names no household", ErrInvalid)
		}
	default:
		return 0, fmt.Errorf("%w: unknown invitation kind %q", ErrInvalid, in.Kind)
	}
	now := s.now().UTC()
	id, err := s.q.InsertInvitation(ctx, householddb.InsertInvitationParams{
		Kind:        string(in.Kind),
		Email:       e,
		HouseholdID: household,
		IssuedBy:    issuer,
		CreatedAt:   now.Format(timeLayout),
		ExpiresAt:   now.Add(ttl).Format(timeLayout),
	})
	if err != nil {
		return 0, fmt.Errorf("insert invitation: %w", err)
	}
	return id, nil
}

// Accept uses invitation id on behalf of the verified email, and returns the
// household the person now belongs to. householdName names the new household
// of a household invitation and is ignored for a join invitation.
//
// The invitation is claimed in one conditional UPDATE, so of two concurrent
// accepts exactly one succeeds. When the person already belongs to a household
// the claim is rolled back with the rest, leaving the invitation usable after
// they leave.
func (s *Store) Accept(ctx context.Context, id int64, email, householdName string) (Household, error) {
	e, err := NormalizeEmail(email)
	if err != nil {
		return Household{}, err
	}
	var h Household
	err = db.WithTx(ctx, s.db, s.q, func(q *householddb.Queries, _ *sql.Tx) error {
		claim, err := q.ClaimInvitation(ctx, householddb.ClaimInvitationParams{
			Now: sql.NullString{String: s.stamp(), Valid: true}, ID: id, Email: e,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return s.whyNotAcceptable(ctx, q, id, e)
		}
		if err != nil {
			return fmt.Errorf("claim invitation %d: %w", id, err)
		}
		if _, err := lookup(ctx, q, e); err == nil {
			return fmt.Errorf("accept invitation %d: %w", id, ErrAlreadyMember)
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if Kind(claim.Kind) == KindHousehold {
			h, err = s.create(ctx, q, householdName, e)
			return err
		}
		row, err := q.GetHouseholdByID(ctx, claim.HouseholdID.Int64)
		if err != nil {
			return fmt.Errorf("load household %d of invitation %d: %w", claim.HouseholdID.Int64, id, err)
		}
		if h, err = toHousehold(row); err != nil {
			return err
		}
		return s.addMember(ctx, q, h.ID, e, s.stamp())
	})
	return h, err
}

// whyNotAcceptable explains a claim that matched nothing. An invitation that
// is missing or addressed to someone else is ErrNotFound either way.
func (s *Store) whyNotAcceptable(ctx context.Context, q *householddb.Queries, id int64, email string) error {
	inv, err := q.GetInvitation(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && inv.Email != email) {
		return fmt.Errorf("%w: invitation %d", ErrNotFound, id)
	}
	if err != nil {
		return fmt.Errorf("load invitation %d: %w", id, err)
	}
	switch {
	case inv.AcceptedAt.Valid:
		return fmt.Errorf("invitation %d: %w", id, ErrUsed)
	case inv.RevokedAt.Valid:
		return fmt.Errorf("invitation %d: %w", id, ErrRevoked)
	default:
		return fmt.Errorf("invitation %d: %w", id, ErrExpired)
	}
}

// Revoke withdraws an invitation before it is accepted. Only the person who
// issued it can; anyone else, and an invitation already used or revoked, gets
// ErrNotFound.
func (s *Store) Revoke(ctx context.Context, id int64, issuedBy string) error {
	issuer, err := NormalizeEmail(issuedBy)
	if err != nil {
		return err
	}
	n, err := s.q.RevokeInvitation(ctx, householddb.RevokeInvitationParams{
		Now: sql.NullString{String: s.stamp(), Valid: true}, ID: id, IssuedBy: issuer,
	})
	if err != nil {
		return fmt.Errorf("revoke invitation %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: invitation %d", ErrNotFound, id)
	}
	return nil
}
