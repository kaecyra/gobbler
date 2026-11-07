// Package profile is the profile service: every operation on a person's
// profile, sessions, personal API keys, connected apps and avatar that the web
// handlers perform, written once (ADR-00043). It is in-process. It is not a
// network API, it speaks neither HTTP nor JSON and it reads no cookie or
// header. It follows the pattern internal/api/households and
// internal/api/catalog set: a method takes the context, then the
// caller.Caller, then its own arguments; it refuses a Caller that is not a
// verified person with ErrNotMember before touching a row; and it returns the
// domain's errors, which internal/web/layout (statusTable) maps for the web.
//
// These are the person's own data (ADR-00032, ADR-00031), so the check is on
// the person and not on a household: a signed-in person who belongs to no
// household still has a profile, sessions and keys. Every method acts on the
// Caller's own person and takes no email, so a request cannot name whose data
// to touch. The two methods that read another person's profile, Subject and
// OpenAvatar, work out how the Caller stands to it and show only what the
// visibility rule lets that relation see. A household credential
// (Caller.HouseholdKey) has no person and is refused everywhere.
//
// The rules (the visibility of a field, what a display name may be, what a
// key's lifetime may be) stay in internal/profile, internal/apikey and
// internal/oauth, which this package wraps. The package re-exports the types,
// errors and constructors its callers need as aliases, so internal/web and
// internal/mcp import this package and not internal/profile; the
// profile-through-api rule in .golangci.yml fails make lint when they do.
// internal/session, internal/apikey and internal/oauth are shared with the
// sign-in, MCP and OAuth adapters and are not fenced.
package profile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/kaecyra/gobbler/internal/api/caller"
	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/oauth"
	domain "github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/session"
)

// Caller is who an operation is for (caller.Caller).
type Caller = caller.Caller

// The profile values the callers of the service work with.
type (
	// Profile is a person's stored profile (profile.Profile).
	Profile = domain.Profile
	// Shown is a profile as one viewer may see it (profile.Shown).
	Shown = domain.Shown
	// Relation is how a viewer stands to the person whose profile they look at
	// (profile.Relation).
	Relation = domain.Relation
	// Store is the profile store (profile.Store). The router builds it with
	// NewStore, so the web and MCP packages name no domain package.
	Store = domain.Store
	// Avatars runs avatar uploads (profile.Avatars). The server builds it over
	// the media store and hands it to the router.
	Avatars = domain.Avatars
	// Job is a person's avatar upload in flight (profile.Job).
	Job = domain.Job
	// Identicon is the default avatar of an email (profile.Identicon).
	Identicon = domain.Identicon
	// Session is one of a person's signed-in sessions (session.Info).
	Session = session.Info
	// Key is a personal API key as the list shows it (apikey.Key).
	Key = apikey.Key
	// Grant is a connected app (oauth.Grant).
	Grant = oauth.Grant
)

// The relations the visibility rule is keyed on (profile.Self and the rest).
const (
	Self          = domain.Self
	SameHousehold = domain.SameHousehold
	Friend        = domain.Friend
	Other         = domain.Other
)

// MaxNameRunes is the longest display name (profile.MaxNameRunes).
const MaxNameRunes = domain.MaxNameRunes

// The states of an avatar upload (profile.JobUploaded and the rest).
const (
	JobUploaded = domain.JobUploaded
	JobQueued   = domain.JobQueued
	JobFailed   = domain.JobFailed
)

// The errors a service method returns, for callers to branch on with
// errors.Is. The ones that come from a domain package are its own values, so
// errors.Is matches either name.
var (
	// ErrNotFound is a profile no address names (profile.ErrNotFound).
	ErrNotFound = domain.ErrNotFound
	// ErrInvalid is input the profile store refuses (profile.ErrInvalid).
	ErrInvalid = domain.ErrInvalid
	// ErrBusy is an avatar upload while the last one is still being processed
	// (profile.ErrBusy).
	ErrBusy = domain.ErrBusy
	// ErrKeyNotFound is a key the person does not have (apikey.ErrNotFound).
	ErrKeyNotFound = apikey.ErrNotFound
	// ErrKeyInvalid is key input the store refuses (apikey.ErrInvalid).
	ErrKeyInvalid = apikey.ErrInvalid
	// ErrGrantNotFound is a connected app the person does not have
	// (oauth.ErrNotFound).
	ErrGrantNotFound = oauth.ErrNotFound
	// ErrNotMember is returned for a Caller that is not a verified person
	// (caller.ErrNotMember).
	ErrNotMember = caller.ErrNotMember
	// ErrNoSessions is a session operation when sign-in sessions are not in
	// use, as under the dev bypass without sign-in.
	ErrNoSessions = errors.New("sign-in sessions are not in use")
	// ErrNoCurrentSession is "end every other session" from a request that
	// carries no live session of its own, where there is no session to keep.
	ErrNoCurrentSession = errors.New("no current session to keep")
)

// NewStore returns the profile store over d, which must already be migrated.
// The router builds it here so that it, too, names no domain package.
func NewStore(d *sql.DB) *Store { return domain.New(d) }

// NewIdenticon draws the default avatar of an email (profile.NewIdenticon).
func NewIdenticon(email string) Identicon { return domain.NewIdenticon(email) }

// InvalidReason returns the message of an error caused by input a store
// refused, whether a display name or a key, and whether err is one. Those
// messages are written for the person who typed the input
// (profile.InvalidReason, apikey.InvalidReason).
func InvalidReason(err error) (string, bool) {
	if reason, ok := domain.InvalidReason(err); ok {
		return reason, true
	}
	return apikey.InvalidReason(err)
}

// Profiles is what the service calls on the profile store. *profile.Store
// satisfies it.
type Profiles interface {
	Ensure(ctx context.Context, email string) (Profile, error)
	ByAddress(ctx context.Context, address string) (Profile, error)
	ByEmail(ctx context.Context, email string) (Profile, error)
	Rename(ctx context.Context, email, name string) error
}

// Households finds the household of an email, to tell whether the viewer and
// the person share one. *household.Store satisfies it.
type Households interface {
	ForEmail(ctx context.Context, email string) (household.Household, error)
}

// Sessions is what the service calls on the session store. *session.Store
// satisfies it.
type Sessions interface {
	ListForEmail(ctx context.Context, email, currentToken string) ([]Session, error)
	End(ctx context.Context, email, id string) (bool, error)
	EndOthers(ctx context.Context, email, keepToken string) (int, error)
}

// Keys is what the service calls on the API key store. *apikey.Store
// satisfies it.
type Keys interface {
	ListPersonal(ctx context.Context, email string) ([]Key, error)
	CreatePersonal(ctx context.Context, email, name string, lifetime time.Duration) (string, error)
	RevokePersonal(ctx context.Context, email string, id int64) error
}

// Grants is what the service calls on the OAuth store. *oauth.Store satisfies
// it.
type Grants interface {
	ListGrants(ctx context.Context, email string) ([]Grant, error)
	RevokeGrant(ctx context.Context, email string, id int64) error
}

// AvatarUploads is what the service calls on the avatar service. *Avatars
// satisfies it.
type AvatarUploads interface {
	Upload(ctx context.Context, email string, data []byte) (Job, error)
	Job(ctx context.Context, email string) (Job, bool, error)
	Preview(ctx context.Context, email, id string) ([]byte, error)
	Crop(ctx context.Context, email, id string, c media.Crop) error
	Open(ctx context.Context, key string) ([]byte, error)
}

// Deps is what the service is built from.
type Deps struct {
	// Profiles, Households, Keys, Grants and Avatars are required.
	Profiles   Profiles
	Households Households
	Keys       Keys
	Grants     Grants
	Avatars    AvatarUploads
	// Sessions is optional: it is nil when sign-in is not mounted, and the
	// session operations then say so (ErrNoSessions).
	Sessions Sessions
}

// Service is the profile operations. It is safe for concurrent use.
type Service struct {
	profiles   Profiles
	households Households
	sessions   Sessions
	keys       Keys
	grants     Grants
	avatars    AvatarUploads
}

// New returns a Service. A missing required dependency is an error here, not a
// nil dereference on the first request.
func New(d Deps) (*Service, error) {
	switch {
	case d.Profiles == nil:
		return nil, errors.New("profile service: profiles are required")
	case d.Households == nil:
		return nil, errors.New("profile service: households are required")
	case d.Keys == nil:
		return nil, errors.New("profile service: keys are required")
	case d.Grants == nil:
		return nil, errors.New("profile service: grants are required")
	case d.Avatars == nil:
		return nil, errors.New("profile service: avatars are required")
	}
	return &Service{
		profiles: d.Profiles, households: d.Households, sessions: d.Sessions,
		keys: d.Keys, grants: d.Grants, avatars: d.Avatars,
	}, nil
}

// person checks that c names a verified person and returns their email as the
// stores key it. It is the first line of every method. It does not look the
// person up in a household: a signed-in person with none has a profile too. A
// household credential is refused: these operations are made as a person.
func person(c Caller) (string, error) {
	email := strings.ToLower(strings.TrimSpace(c.Person))
	if email == "" || c.HouseholdKey {
		return "", fmt.Errorf("no person on the request: %w", ErrNotMember)
	}
	return email, nil
}

// Subject is the profile a request is about and how its viewer stands to the
// person.
type Subject struct {
	Profile  Profile
	Viewer   string
	Relation Relation
}

// Self reports whether the viewer is the person whose profile it is.
func (s Subject) Self() bool { return s.Relation == Self }

// Own returns the Caller's own profile, creating it the first time.
func (s *Service) Own(ctx context.Context, c Caller) (Profile, error) {
	email, err := person(c)
	if err != nil {
		return Profile{}, err
	}
	return s.profiles.Ensure(ctx, email)
}

// Mine returns the Caller's own profile without creating it: ErrNotFound for a
// person who never opened theirs. A page that only wants the avatar can ask on
// every request.
func (s *Service) Mine(ctx context.Context, c Caller) (Profile, error) {
	email, err := person(c)
	if err != nil {
		return Profile{}, err
	}
	return s.profiles.ByEmail(ctx, email)
}

// Subject loads the profile the address names and works out how the Caller
// stands to its person. An address that names nothing is ErrNotFound for
// everyone: the answer says nothing about which addresses exist.
func (s *Service) Subject(ctx context.Context, c Caller, address string) (Subject, error) {
	viewer, err := person(c)
	if err != nil {
		return Subject{}, err
	}
	p, err := s.profiles.ByAddress(ctx, address)
	if err != nil {
		return Subject{}, err
	}
	rel, err := s.relation(ctx, viewer, p.Email)
	if err != nil {
		return Subject{}, err
	}
	return Subject{Profile: p, Viewer: viewer, Relation: rel}, nil
}

// relation is how the viewer stands to the person. Both households are read
// from the store. Someone with no household shares one with nobody. A lookup
// that fails is a failure, not "not the same household", so it cannot quietly
// decide what a viewer may see.
func (s *Service) relation(ctx context.Context, viewer, person string) (Relation, error) {
	if rel := domain.RelationBetween(viewer, person, false); rel == Self {
		return rel, nil
	}
	mine, ok, err := s.householdOf(ctx, viewer)
	if err != nil {
		return "", fmt.Errorf("household of the viewer: %w", err)
	}
	if !ok {
		return domain.RelationBetween(viewer, person, false), nil
	}
	theirs, ok, err := s.householdOf(ctx, person)
	if err != nil {
		return "", fmt.Errorf("household of the profile's person: %w", err)
	}
	return domain.RelationBetween(viewer, person, ok && theirs.ID == mine.ID), nil
}

// householdOf is the household of an email, and false for someone in none.
func (s *Service) householdOf(ctx context.Context, email string) (household.Household, bool, error) {
	h, err := s.households.ForEmail(ctx, email)
	switch {
	case err == nil:
		return h, true, nil
	case errors.Is(err, household.ErrNotFound), errors.Is(err, household.ErrInvalid):
		return household.Household{}, false, nil
	default:
		return household.Household{}, false, err
	}
}

// HouseholdName is the name of the Caller's household, empty for a person in
// none. A failed lookup is an error, not "no household".
func (s *Service) HouseholdName(ctx context.Context, c Caller) (string, error) {
	email, err := person(c)
	if err != nil {
		return "", err
	}
	h, ok, err := s.householdOf(ctx, email)
	if err != nil {
		return "", fmt.Errorf("household of the profile's person: %w", err)
	}
	if !ok {
		return "", nil
	}
	return h.Name, nil
}

// Rename sets the Caller's display name (profile.Store.Rename).
func (s *Service) Rename(ctx context.Context, c Caller, name string) error {
	email, err := person(c)
	if err != nil {
		return err
	}
	return s.profiles.Rename(ctx, email, name)
}

// ListSessions lists the Caller's live sessions, newest first, with the one
// currentToken names marked. The bool is false when sign-in sessions are not in
// use at all, where there is nothing to list.
func (s *Service) ListSessions(ctx context.Context, c Caller, currentToken string) ([]Session, bool, error) {
	email, err := person(c)
	if err != nil {
		return nil, false, err
	}
	if s.sessions == nil {
		return nil, false, nil
	}
	list, err := s.sessions.ListForEmail(ctx, email, currentToken)
	if err != nil {
		return nil, true, fmt.Errorf("list sessions: %w", err)
	}
	return list, true, nil
}

// EndSession ends the Caller's session that id names. One that is not theirs,
// or is already gone, ends nothing and is not an error.
func (s *Service) EndSession(ctx context.Context, c Caller, id string) error {
	email, err := person(c)
	if err != nil {
		return err
	}
	if s.sessions == nil {
		return ErrNoSessions
	}
	_, err = s.sessions.End(ctx, email, id)
	return err
}

// EndOtherSessions ends every session of the Caller except the one
// currentToken names. The token must be one of the Caller's live sessions, or
// "every other" would be every one: otherwise it is ErrNoCurrentSession.
func (s *Service) EndOtherSessions(ctx context.Context, c Caller, currentToken string) error {
	email, err := person(c)
	if err != nil {
		return err
	}
	if s.sessions == nil {
		return ErrNoSessions
	}
	list, err := s.sessions.ListForEmail(ctx, email, currentToken)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	if !slices.ContainsFunc(list, func(i Session) bool { return i.Current }) {
		return ErrNoCurrentSession
	}
	_, err = s.sessions.EndOthers(ctx, email, currentToken)
	return err
}

// ListKeys lists the Caller's personal API keys.
func (s *Service) ListKeys(ctx context.Context, c Caller) ([]Key, error) {
	email, err := person(c)
	if err != nil {
		return nil, err
	}
	return s.keys.ListPersonal(ctx, email)
}

// CreateKey makes a personal API key for the Caller named name that lasts for
// lifetime (one of apikey.LifetimeFor's values) and returns its secret, which
// is never available again.
func (s *Service) CreateKey(ctx context.Context, c Caller, name, lifetime string) (string, error) {
	email, err := person(c)
	if err != nil {
		return "", err
	}
	life, err := apikey.LifetimeFor(lifetime)
	if err != nil {
		return "", err
	}
	return s.keys.CreatePersonal(ctx, email, name, life.Duration())
}

// RevokeKey revokes the Caller's personal key id. Someone else's key and one
// that does not exist are both ErrKeyNotFound.
func (s *Service) RevokeKey(ctx context.Context, c Caller, id int64) error {
	email, err := person(c)
	if err != nil {
		return err
	}
	return s.keys.RevokePersonal(ctx, email, id)
}

// ListGrants lists the apps the Caller has connected.
func (s *Service) ListGrants(ctx context.Context, c Caller) ([]Grant, error) {
	email, err := person(c)
	if err != nil {
		return nil, err
	}
	return s.grants.ListGrants(ctx, email)
}

// RevokeGrant revokes the Caller's grant id. Someone else's grant and one that
// does not exist are both ErrGrantNotFound.
func (s *Service) RevokeGrant(ctx context.Context, c Caller, id int64) error {
	email, err := person(c)
	if err != nil {
		return err
	}
	return s.grants.RevokeGrant(ctx, email, id)
}

// UploadAvatar takes the Caller's picture and starts processing it
// (profile.Avatars.Upload).
func (s *Service) UploadAvatar(ctx context.Context, c Caller, data []byte) (Job, error) {
	email, err := person(c)
	if err != nil {
		return Job{}, err
	}
	return s.avatars.Upload(ctx, email, data)
}

// AvatarJob is the Caller's avatar upload in flight, and false when there is
// none.
func (s *Service) AvatarJob(ctx context.Context, c Caller) (Job, bool, error) {
	email, err := person(c)
	if err != nil {
		return Job{}, false, err
	}
	return s.avatars.Job(ctx, email)
}

// AvatarPreview is the Caller's uploaded picture the crop is chosen against.
func (s *Service) AvatarPreview(ctx context.Context, c Caller, id string) ([]byte, error) {
	email, err := person(c)
	if err != nil {
		return nil, err
	}
	return s.avatars.Preview(ctx, email, id)
}

// CropAvatar takes the Caller's chosen rectangle of upload id and queues the
// picture for processing.
func (s *Service) CropAvatar(ctx context.Context, c Caller, id string, crop media.Crop) error {
	email, err := person(c)
	if err != nil {
		return err
	}
	return s.avatars.Crop(ctx, email, id, crop)
}

// AvatarKey is the key of the avatar the Subject's viewer may see, empty when
// the profile has none or the visibility rule hides it. Every view of a
// profile field is filtered by the same rule (ADR-00032).
func AvatarKey(s Subject) string { return s.Profile.As(s.Relation).Avatar }

// OpenAvatar reads the avatar of the Subject's profile, if the Caller is the
// viewer it was worked out for and the visibility rule shows them one. A viewer
// who may not see it gets the same ErrNotFound as a profile with none.
func (s *Service) OpenAvatar(ctx context.Context, c Caller, sub Subject) ([]byte, error) {
	email, err := person(c)
	if err != nil {
		return nil, err
	}
	if email != sub.Viewer {
		return nil, fmt.Errorf("avatar subject was made for another viewer: %w", ErrNotMember)
	}
	key := AvatarKey(sub)
	if key == "" {
		return nil, fmt.Errorf("profile has no visible avatar: %w", ErrNotFound)
	}
	return s.avatars.Open(ctx, key)
}
