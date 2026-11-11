// Package households is the household service: every operation on a
// household's members and invitations that the web handlers perform, written
// once (ADR-00043). It is in-process. It is not a network API, it speaks
// neither HTTP nor JSON and it reads no cookie or header. It follows the
// pattern internal/api/catalog and internal/api/sharing set: a method takes the
// context, then the caller.Caller, then its own arguments; it refuses a Caller
// that is not a verified member of the household with ErrNotMember before
// touching a row (ADR-00018); and it returns the domain's errors, which
// internal/web/layout (statusTable) maps for the web.
//
// The rules (who may be invited, what an invitation does, who may leave and
// delete) stay in internal/household, which this package wraps. The package
// re-exports the types, errors and context helpers its callers need as
// aliases, so internal/web and internal/mcp import this package and not
// internal/household; the households-through-api rule in .golangci.yml fails
// make lint when they do.
//
// Two groups of methods check something other than membership.
//
// InvitationFor and Accept are for a verified person who is not (yet) a member:
// an invitation is how one becomes a member (ADR-00028). They need only the
// Caller's Person, and the domain refuses an invitation that is not addressed
// to that person.
//
// The Operator* methods are the operator's invitations to new households
// (ADR-00018). They reach across households, so they check the Caller's person
// against the configured operators instead of membership (ADR-00043): an
// operator need not be a member of any household.
//
// The household acted in comes from the membership check, never from the
// Caller or the context, and every member action is made as a person: a
// household credential (Caller.HouseholdKey) has no person to invite, remove or
// delete as, and is refused. The one read it may make is HouseStyle, which an
// agent reads over MCP (ADR-00048).
package households

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kaecyra/gobbler/internal/api/caller"
	domain "github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/mail"
)

// Caller is who an operation is for (caller.Caller).
type Caller = caller.Caller

// The household values the callers of the service work with.
type (
	// Household is a tenant (household.Household). It is also the value the
	// membership middleware puts on a request's context, so FromContext and
	// WithContext are re-exported with it.
	Household = domain.Household
	// Invitation is an invitation as the pages show it (household.Invitation).
	Invitation = domain.Invitation
	// Kind says what accepting an invitation does (household.Kind).
	Kind = domain.Kind
	// Store is the household store (household.Store). The router builds it with
	// NewStore, so the web and MCP packages name no domain package.
	Store = domain.Store
	// PictureReaper finds and deletes a household's picture objects
	// (household.PictureReaper).
	PictureReaper = domain.PictureReaper
	// LeaveHook acts on an email that has just stopped being a member
	// (household.LeaveHook).
	LeaveHook = domain.LeaveHook
)

// The invitation kinds (household.KindJoin, household.KindHousehold).
const (
	KindJoin      = domain.KindJoin
	KindHousehold = domain.KindHousehold
)

// The errors a service method returns, for callers to branch on with
// errors.Is. They are the domain's own values, so errors.Is matches either name.
var (
	// ErrNotFound is a household with no such member, or an invitation that does
	// not exist or is not addressed to the person (household.ErrNotFound).
	ErrNotFound = domain.ErrNotFound
	// ErrInvalid is input the store refuses (household.ErrInvalid).
	ErrInvalid = domain.ErrInvalid
	// ErrAlreadyMember is a person who must leave their household first
	// (household.ErrAlreadyMember).
	ErrAlreadyMember = domain.ErrAlreadyMember
	// ErrExpired is an invitation past its expiry (household.ErrExpired).
	ErrExpired = domain.ErrExpired
	// ErrUsed is an invitation already accepted (household.ErrUsed).
	ErrUsed = domain.ErrUsed
	// ErrRevoked is an invitation its issuer revoked (household.ErrRevoked).
	ErrRevoked = domain.ErrRevoked
	// ErrLastMember is the only member trying to leave (household.ErrLastMember).
	ErrLastMember = domain.ErrLastMember
	// ErrNotLastMember is a delete while others remain
	// (household.ErrNotLastMember).
	ErrNotLastMember = domain.ErrNotLastMember
	// ErrConfirmation is a deletion confirmed with the wrong name
	// (household.ErrConfirmation).
	ErrConfirmation = domain.ErrConfirmation
	// ErrNotMember is returned for a Caller that is not a verified member of a
	// household, and for an operation of the operator by someone who is not one
	// (caller.ErrNotMember).
	ErrNotMember = caller.ErrNotMember
)

// MaxHouseStyleBytes is the longest house style (household.MaxHouseStyleBytes).
const MaxHouseStyleBytes = domain.MaxHouseStyleBytes

// WithContext returns ctx carrying h, the household the membership middleware
// admitted the request to (household.WithContext). Only an adapter that has
// verified the membership calls it.
func WithContext(ctx context.Context, h Household) context.Context {
	return domain.WithContext(ctx, h)
}

// FromContext returns the household on ctx, if the request was admitted to one
// (household.FromContext).
func FromContext(ctx context.Context) (Household, bool) { return domain.FromContext(ctx) }

// ValidCode reports whether s is shaped like an invitation code
// (household.ValidCode).
func ValidCode(s string) bool { return domain.ValidCode(s) }

// InvalidReason returns the message of an error caused by input the store
// refused, and whether err is one (household.InvalidReason).
func InvalidReason(err error) (string, bool) { return domain.InvalidReason(err) }

// NewStore returns the household store over d, which must already be migrated.
// The router builds it here so that it, too, names no domain package.
func NewStore(d *sql.DB) *Store { return domain.New(d) }

// Households is what the service calls on the household store. *household.Store
// satisfies it. ForEmail is also the membership lookup every method checks the
// Caller against (caller.Enter), so the two cannot disagree.
type Households interface {
	ForEmail(ctx context.Context, email string) (Household, error)
	Members(ctx context.Context, householdID int64) ([]string, error)
	PendingForHousehold(ctx context.Context, householdID int64) ([]Invitation, error)
	PendingHouseholdInvitations(ctx context.Context, issuer string) ([]Invitation, error)
	Invite(ctx context.Context, in domain.Invite) (string, error)
	Revoke(ctx context.Context, code, issuedBy string) error
	Remove(ctx context.Context, householdID int64, by, target string) error
	Leave(ctx context.Context, email string) error
	Delete(ctx context.Context, householdID int64, by, confirm string) error
	InvitationFor(ctx context.Context, code, email string) (Invitation, error)
	PendingJoinForSend(ctx context.Context, householdID int64, code string) (Invitation, error)
	PendingHouseholdForSend(ctx context.Context, issuer, code string) (Invitation, error)
	RecordEmail(ctx context.Context, code string, sendErr error) error
	Accept(ctx context.Context, code, email, householdName string) (Household, error)
	HouseStyle(ctx context.Context, householdID int64) (string, error)
	SetHouseStyle(ctx context.Context, householdID int64, style string) error
}

// Operators says who the operators are. config.Operator satisfies it.
type Operators interface {
	IsOperator(email string) bool
}

// Mailer emails an invitation (ADR-00055). *mail.Mailer satisfies it. It
// returns mail.ErrNotConfigured, wrapped, when mail is off.
type Mailer interface {
	SendInvitation(ctx context.Context, in mail.InvitationEmail) error
}

// Deps is what the service is built from.
type Deps struct {
	// Households is the household store. Required.
	Households Households
	// Operators decides who may use the Operator* methods. Optional: without it
	// nobody is an operator and they all refuse.
	Operators Operators
	// Mailer emails invitations when they are issued and resent. Optional:
	// without it, or while it reports mail off, invitations are only created.
	Mailer Mailer
	// Logger records failed sends. Optional; slog.Default when nil.
	Logger *slog.Logger
}

// Service is the household operations. It is safe for concurrent use.
type Service struct {
	store  Households
	ops    Operators
	mailer Mailer
	log    *slog.Logger
}

// New returns a Service. Households is required; a missing one is an error
// here, not a nil dereference on the first request.
func New(d Deps) (*Service, error) {
	if d.Households == nil {
		return nil, errors.New("households service: households are required")
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: d.Households, ops: d.Operators, mailer: d.Mailer, log: log}, nil
}

// member checks that c is a person who is a member of the household it names
// (caller.Enter) and returns a context that runs in it. It is the first line of
// every member operation. A household credential is refused: these operations
// are made as a person.
func (s *Service) member(ctx context.Context, c Caller) (context.Context, Household, error) {
	if c.HouseholdKey {
		return nil, Household{}, fmt.Errorf("a household key acts as no person: %w", ErrNotMember)
	}
	ctx, err := caller.Enter(ctx, s.store, c)
	if err != nil {
		return nil, Household{}, err
	}
	h, ok := domain.FromContext(ctx)
	if !ok {
		return nil, Household{}, fmt.Errorf("membership check left no household: %w", ErrNotMember)
	}
	return ctx, h, nil
}

// Members lists the emails of the Caller's household.
func (s *Service) Members(ctx context.Context, c Caller) ([]string, error) {
	ctx, h, err := s.member(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.store.Members(ctx, h.ID)
}

// Pending lists the join invitations into the Caller's household that can still
// be accepted, oldest first.
func (s *Service) Pending(ctx context.Context, c Caller) ([]Invitation, error) {
	ctx, h, err := s.member(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.store.PendingForHousehold(ctx, h.ID)
}

// Invite issues a join invitation into the Caller's household for email, and
// returns its code (household.Store.Invite). When mail is configured the link
// is emailed during the call; a send that fails, or is refused by the rate,
// leaves the invitation created with the failure stored on it (ADR-00055).
func (s *Service) Invite(ctx context.Context, c Caller, email string) (string, error) {
	ctx, h, err := s.member(ctx, c)
	if err != nil {
		return "", err
	}
	code, err := s.store.Invite(ctx, domain.Invite{Kind: KindJoin, Email: email, HouseholdID: h.ID, IssuedBy: c.Person})
	if err != nil {
		return "", err
	}
	if inv, err := s.store.PendingJoinForSend(ctx, h.ID, code); err != nil {
		s.log.ErrorContext(ctx, "invitation created but not read back to email",
			"household_id", h.ID, "invited_domain", emailDomain(email), "error", err.Error())
	} else {
		s.deliver(ctx, c.Person, inv, false)
	}
	return code, nil
}

// Resend emails the pending join invitation code of the Caller's household
// again. Any member may; the send counts against the Caller's rate. The
// outcome replaces the invitation's stored status. An invitation that is not
// pending in the Caller's household is ErrNotFound.
func (s *Service) Resend(ctx context.Context, c Caller, code string) error {
	ctx, h, err := s.member(ctx, c)
	if err != nil {
		return err
	}
	inv, err := s.store.PendingJoinForSend(ctx, h.ID, code)
	if err != nil {
		return err
	}
	s.deliver(ctx, c.Person, inv, true)
	return nil
}

// emailDomain is the part of an address after the @, for a log line: it says
// where an invitation went without naming the person, and an invitation code is
// the credential that admits them, so it is never logged (ADR-00028).
func emailDomain(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 {
		return email[i+1:]
	}
	return ""
}

// deliver emails inv as sender and records the outcome on it. Mail being off
// is not a failure of an invite and records nothing; for a resend it records
// that email is not set up, because the person asked for an email and should be
// told. No failure is returned: the invitation exists either way.
//
// The stored error is mail.Reason, a fixed sentence chosen by the kind of
// failure: every member of the household sees it, and the raw error can carry
// the operator's SMTP user, server names or the provider's reply. The raw error
// goes to the log, with the household and the invited address's domain and
// never the invitation code.
func (s *Service) deliver(ctx context.Context, sender string, inv Invitation, resend bool) {
	var err error
	if s.mailer == nil {
		err = fmt.Errorf("no mailer: %w", mail.ErrNotConfigured)
	} else {
		err = s.mailer.SendInvitation(ctx, mail.InvitationEmail{
			Sender: sender, InvitedBy: inv.IssuedBy, To: inv.Email, Household: inv.HouseholdName,
			Code: inv.Code, Expires: inv.ExpiresAt,
		})
	}
	if errors.Is(err, mail.ErrNotConfigured) && !resend {
		return
	}
	var stored error
	if err != nil {
		s.log.WarnContext(ctx, "invitation email not sent",
			"household_id", inv.HouseholdID, "invited_domain", emailDomain(inv.Email), "error", err.Error())
		stored = errors.New(mail.Reason(err))
	}
	if rerr := s.store.RecordEmail(ctx, inv.Code, stored); rerr != nil {
		s.log.ErrorContext(ctx, "could not record the outcome of an invitation email",
			"household_id", inv.HouseholdID, "invited_domain", emailDomain(inv.Email), "error", rerr.Error())
	}
}

// Revoke revokes the invitation code, which only its issuer, the Caller, can.
func (s *Service) Revoke(ctx context.Context, c Caller, code string) error {
	ctx, _, err := s.member(ctx, c)
	if err != nil {
		return err
	}
	return s.store.Revoke(ctx, code, c.Person)
}

// Remove takes target out of the Caller's household.
func (s *Service) Remove(ctx context.Context, c Caller, target string) error {
	ctx, h, err := s.member(ctx, c)
	if err != nil {
		return err
	}
	return s.store.Remove(ctx, h.ID, c.Person, target)
}

// Leave takes the Caller out of their household.
func (s *Service) Leave(ctx context.Context, c Caller) error {
	ctx, _, err := s.member(ctx, c)
	if err != nil {
		return err
	}
	return s.store.Leave(ctx, c.Person)
}

// Delete deletes the Caller's household, which only its last member can, and
// only by typing its name as confirm.
func (s *Service) Delete(ctx context.Context, c Caller, confirm string) error {
	ctx, h, err := s.member(ctx, c)
	if err != nil {
		return err
	}
	return s.store.Delete(ctx, h.ID, c.Person, confirm)
}

// HouseStyle returns the house style of the Caller's household (ADR-00048),
// empty when none has been written. Unlike the member operations it admits a
// household credential (caller.Enter): reading the house style is a read of
// the household, which an agent on a household key needs before it revises a
// draft, and it acts as no person.
func (s *Service) HouseStyle(ctx context.Context, c Caller) (string, error) {
	ctx, err := caller.Enter(ctx, s.store, c)
	if err != nil {
		return "", err
	}
	h, ok := domain.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("membership check left no household: %w", ErrNotMember)
	}
	return s.store.HouseStyle(ctx, h.ID)
}

// SetHouseStyle replaces the house style of the Caller's household and changes
// nothing else (ADR-00048). Any member may; a style over MaxHouseStyleBytes is
// ErrInvalid. It is made as a person, so a household credential is refused: no
// agent writes the rules that steer its own edits.
func (s *Service) SetHouseStyle(ctx context.Context, c Caller, style string) error {
	ctx, h, err := s.member(ctx, c)
	if err != nil {
		return err
	}
	return s.store.SetHouseStyle(ctx, h.ID, style)
}

// person checks that c names a verified person. It does not look the person up:
// the operations that use it are for someone who may not be a member.
func person(c Caller) error {
	if c.Person == "" || c.HouseholdKey {
		return fmt.Errorf("no person on the request: %w", ErrNotMember)
	}
	return nil
}

// InvitationFor returns the invitation code as it is shown to the Caller's
// person, who need not be a member (ADR-00028). An invitation that does not
// exist or is not addressed to the person is ErrNotFound alike.
func (s *Service) InvitationFor(ctx context.Context, c Caller, code string) (Invitation, error) {
	if err := person(c); err != nil {
		return Invitation{}, err
	}
	return s.store.InvitationFor(ctx, code, c.Person)
}

// Accept accepts the invitation code as the Caller's person, who must not
// already be a member, and returns the household they now belong to. A
// household invitation creates the household, named name.
func (s *Service) Accept(ctx context.Context, c Caller, code, name string) (Household, error) {
	if err := person(c); err != nil {
		return Household{}, err
	}
	return s.store.Accept(ctx, code, c.Person, name)
}

// operator admits an operator. The check is on the Caller's person, not on
// membership, and not on the context: an operator reaches across households and
// need not be in one (ADR-00043). Anyone else, and a Caller with no person, is
// refused with ErrNotMember, which the web answers with 403 like the operator
// pages do for a non-operator.
func (s *Service) operator(c Caller) error {
	if c.Person == "" || c.HouseholdKey || s.ops == nil || !s.ops.IsOperator(c.Person) {
		return fmt.Errorf("not an operator: %w", ErrNotMember)
	}
	return nil
}

// OperatorPending lists the household invitations the operator issued that can
// still be accepted.
func (s *Service) OperatorPending(ctx context.Context, c Caller) ([]Invitation, error) {
	if err := s.operator(c); err != nil {
		return nil, err
	}
	return s.store.PendingHouseholdInvitations(ctx, c.Person)
}

// OperatorInvite invites email to start a household and returns the
// invitation's code. It emails the link as Invite does.
func (s *Service) OperatorInvite(ctx context.Context, c Caller, email string) (string, error) {
	if err := s.operator(c); err != nil {
		return "", err
	}
	code, err := s.store.Invite(ctx, domain.Invite{Kind: KindHousehold, Email: email, IssuedBy: c.Person})
	if err != nil {
		return "", err
	}
	if inv, err := s.store.PendingHouseholdForSend(ctx, c.Person, code); err != nil {
		s.log.ErrorContext(ctx, "invitation created but not read back to email",
			"invited_domain", emailDomain(email), "error", err.Error())
	} else {
		s.deliver(ctx, c.Person, inv, false)
	}
	return code, nil
}

// OperatorResend emails the pending household invitation code, which the
// Caller issued, again.
func (s *Service) OperatorResend(ctx context.Context, c Caller, code string) error {
	if err := s.operator(c); err != nil {
		return err
	}
	inv, err := s.store.PendingHouseholdForSend(ctx, c.Person, code)
	if err != nil {
		return err
	}
	s.deliver(ctx, c.Person, inv, true)
	return nil
}

// OperatorRevoke revokes the household invitation code, which only the
// operator who issued it can.
func (s *Service) OperatorRevoke(ctx context.Context, c Caller, code string) error {
	if err := s.operator(c); err != nil {
		return err
	}
	return s.store.Revoke(ctx, code, c.Person)
}
