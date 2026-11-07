package profile_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/api/profile"
	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/media"
	"github.com/kaecyra/gobbler/internal/oauth"
	domain "github.com/kaecyra/gobbler/internal/profile"
	"github.com/kaecyra/gobbler/internal/session"
)

const (
	annEmail  = "ann@example.com"
	bobEmail  = "bob@example.com"
	cyEmail   = "cy@example.com"
	loneEmail = "lone@example.com"
)

type fixture struct {
	svc                *profile.Service
	sessions           *session.Store
	media              media.Store
	profiles           profile.Profiles
	households         profile.Households
	keys               profile.Keys
	grants             profile.Grants
	avatars            profile.AvatarUploads
	ann, bob, cy, lone profile.Caller
}

// newFixture has ann and bob in one household, cy in another and lone in none.
func newFixture(t *testing.T, withSessions bool) fixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatal(err)
	}
	hs := household.New(d)
	smiths, err := hs.Create(ctx, "Smiths", annEmail)
	if err != nil {
		t.Fatal(err)
	}
	code, err := hs.Invite(ctx, household.Invite{Kind: household.KindJoin, Email: bobEmail, HouseholdID: smiths.ID, IssuedBy: annEmail})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hs.Accept(ctx, code, bobEmail, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := hs.Create(ctx, "Joneses", cyEmail); err != nil {
		t.Fatal(err)
	}
	ps := domain.New(d)
	ms := media.NewLocal(t.TempDir())
	deps := profile.Deps{
		Profiles: ps, Households: hs, Keys: apikey.New(d), Grants: oauth.New(d),
		Avatars: domain.NewAvatars(ctx, ps, ms, nil),
	}
	f := fixture{
		media: ms, profiles: ps, households: hs, keys: deps.Keys, grants: deps.Grants, avatars: deps.Avatars,
		ann: profile.Caller{Person: annEmail}, bob: profile.Caller{Person: bobEmail},
		cy: profile.Caller{Person: cyEmail}, lone: profile.Caller{Person: loneEmail},
	}
	if withSessions {
		if f.sessions, err = session.New(d, time.Hour); err != nil {
			t.Fatal(err)
		}
		deps.Sessions = f.sessions
	}
	if f.svc, err = profile.New(deps); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestNewRequiresItsStores(t *testing.T) {
	if _, err := profile.New(profile.Deps{}); err == nil {
		t.Fatal("New without stores = nil, want an error")
	}
}

// Every operation, as a function of the Caller it is asked for.
func ops(f fixture) map[string]func(c profile.Caller) error {
	ctx := context.Background()
	return map[string]func(c profile.Caller) error{
		"Own":              func(c profile.Caller) error { _, err := f.svc.Own(ctx, c); return err },
		"Mine":             func(c profile.Caller) error { _, err := f.svc.Mine(ctx, c); return err },
		"Subject":          func(c profile.Caller) error { _, err := f.svc.Subject(ctx, c, "x"); return err },
		"HouseholdName":    func(c profile.Caller) error { _, err := f.svc.HouseholdName(ctx, c); return err },
		"Rename":           func(c profile.Caller) error { return f.svc.Rename(ctx, c, "x") },
		"ListSessions":     func(c profile.Caller) error { _, _, err := f.svc.ListSessions(ctx, c, ""); return err },
		"EndSession":       func(c profile.Caller) error { return f.svc.EndSession(ctx, c, "x") },
		"EndOtherSessions": func(c profile.Caller) error { return f.svc.EndOtherSessions(ctx, c, "x") },
		"ListKeys":         func(c profile.Caller) error { _, err := f.svc.ListKeys(ctx, c); return err },
		"CreateKey":        func(c profile.Caller) error { _, err := f.svc.CreateKey(ctx, c, "k", "90d"); return err },
		"RevokeKey":        func(c profile.Caller) error { return f.svc.RevokeKey(ctx, c, 1) },
		"ListGrants":       func(c profile.Caller) error { _, err := f.svc.ListGrants(ctx, c); return err },
		"RevokeGrant":      func(c profile.Caller) error { return f.svc.RevokeGrant(ctx, c, 1) },
		"UploadAvatar":     func(c profile.Caller) error { _, err := f.svc.UploadAvatar(ctx, c, []byte("x")); return err },
		"AvatarJob":        func(c profile.Caller) error { _, _, err := f.svc.AvatarJob(ctx, c); return err },
		"AvatarPreview":    func(c profile.Caller) error { _, err := f.svc.AvatarPreview(ctx, c, "x"); return err },
		"CropAvatar":       func(c profile.Caller) error { return f.svc.CropAvatar(ctx, c, "x", media.Crop{}) },
		"OpenAvatar":       func(c profile.Caller) error { _, err := f.svc.OpenAvatar(ctx, c, "x"); return err },
	}
}

// These are a person's own data: no person, and a household credential, which
// is no person, are refused before any row is touched.
func TestEveryOperationRefusesACallerWithNoPerson(t *testing.T) {
	f := newFixture(t, true)
	for name, op := range ops(f) {
		for who, c := range map[string]profile.Caller{
			"no person":       {},
			"blank person":    {Person: "  "},
			"household key":   {Person: "", HouseholdKey: true, Household: household.Household{ID: 1}},
			"key with a name": {Person: annEmail, HouseholdKey: true},
		} {
			if err := op(c); !errors.Is(err, profile.ErrNotMember) {
				t.Errorf("%s as %s = %v, want ErrNotMember", name, who, err)
			}
		}
	}
}

// A person in no household is not refused: they have a profile too.
func TestAPersonInNoHouseholdHasAProfile(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	p, err := f.svc.Own(ctx, f.lone)
	if err != nil || p.Email != loneEmail || p.Address == "" {
		t.Fatalf("Own = %+v, %v", p, err)
	}
	if name, err := f.svc.HouseholdName(ctx, f.lone); err != nil || name != "" {
		t.Errorf("HouseholdName of a person in none = %q, %v", name, err)
	}
	if name, err := f.svc.HouseholdName(ctx, f.ann); err != nil || name != "Smiths" {
		t.Errorf("HouseholdName = %q, %v", name, err)
	}
}

func TestMineDoesNotCreateAProfile(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	if _, err := f.svc.Mine(ctx, f.ann); !errors.Is(err, profile.ErrNotFound) {
		t.Fatalf("Mine before Own = %v, want ErrNotFound", err)
	}
	own, _ := f.svc.Own(ctx, f.ann)
	if got, err := f.svc.Mine(ctx, f.ann); err != nil || got.Address != own.Address {
		t.Errorf("Mine after Own = %+v, %v", got, err)
	}
}

// ADR-00032 obligation 1: the visibility rule is asked with the right relation.
func TestSubjectWorksOutTheRelation(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	addr := map[string]string{}
	for _, c := range []profile.Caller{f.ann, f.bob, f.cy, f.lone} {
		p, err := f.svc.Own(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		addr[c.Person] = p.Address
	}
	cases := []struct {
		name   string
		viewer profile.Caller
		person string
		want   profile.Relation
	}{
		{"self", f.ann, annEmail, profile.Self},
		{"self without a household", f.lone, loneEmail, profile.Self},
		{"same household", f.ann, bobEmail, profile.SameHousehold},
		{"another household", f.ann, cyEmail, profile.Other},
		{"a person with no household", f.ann, loneEmail, profile.Other},
		{"a viewer with no household", f.lone, annEmail, profile.Other},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := f.svc.Subject(ctx, tc.viewer, addr[tc.person])
			if err != nil || s.Relation != tc.want || s.Profile.Email != tc.person || s.Viewer != tc.viewer.Person {
				t.Errorf("Subject = %+v, %v; want relation %q", s, err, tc.want)
			}
		})
	}
	if _, err := f.svc.Subject(ctx, f.ann, "AAAAAAAAAAAAAAAAAAAAAA"); !errors.Is(err, profile.ErrNotFound) {
		t.Errorf("a guessed address = %v, want ErrNotFound", err)
	}
}

// failsFor succeeds for every email but one.
type failsFor struct {
	fail string
	err  error
	ok   household.Household
}

func (f failsFor) ForEmail(_ context.Context, email string) (household.Household, error) {
	if email == f.fail {
		return household.Household{}, f.err
	}
	return f.ok, nil
}

// A household lookup that fails is a failure, not "not the same household": it
// must not quietly decide what a viewer may see. The viewer's lookup and the
// profile person's lookup are two branches, and each must report.
func TestSubjectReportsAFailingHouseholdLookup(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	boom := errors.New("database is locked")
	build := func(fail string) *profile.Service {
		svc, err := profile.New(profile.Deps{
			Profiles:   failingProfiles{},
			Households: failsFor{fail: fail, err: boom, ok: household.Household{ID: 1}},
			Keys:       f.keys, Grants: f.grants, Avatars: f.avatars,
		})
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	for name, fail := range map[string]string{"viewer": bobEmail, "profile person": annEmail} {
		if _, err := build(fail).Subject(ctx, f.bob, "addr"); !errors.Is(err, boom) {
			t.Errorf("Subject with the %s's lookup failing = %v, want the lookup's error", name, err)
		}
	}
	// Self needs no lookup, so a failing store does not break one's own page.
	svc := build(annEmail)
	if s, err := svc.Subject(ctx, profile.Caller{Person: "Ann@Example.com"}, "addr"); err != nil || s.Relation != profile.Self {
		t.Errorf("own profile with a failing store = %+v, %v", s, err)
	}
}

type failingProfiles struct{ profile.Profiles }

func (failingProfiles) ByAddress(context.Context, string) (profile.Profile, error) {
	return profile.Profile{Email: annEmail, Address: "addr"}, nil
}

func TestRenameActsOnTheCallerAlone(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	if _, err := f.svc.Own(ctx, f.bob); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Own(ctx, f.ann); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Rename(ctx, profile.Caller{Person: "  Ann@Example.com "}, "Ann B"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Mine(ctx, f.ann); got.DisplayName != "Ann B" {
		t.Errorf("ann's name = %q", got.DisplayName)
	}
	if got, _ := f.svc.Mine(ctx, f.bob); got.DisplayName != "" {
		t.Errorf("bob's name = %q, want it untouched", got.DisplayName)
	}
	if err := f.svc.Rename(ctx, f.ann, string(make([]rune, 61))); !errors.Is(err, profile.ErrInvalid) {
		t.Errorf("a too long name = %v, want ErrInvalid", err)
	}
}

func TestSessions(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	cur, err := f.sessions.Create(ctx, annEmail, session.Client{UserAgent: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.Create(ctx, annEmail, session.Client{UserAgent: "b"}); err != nil {
		t.Fatal(err)
	}
	other, err := f.sessions.Create(ctx, bobEmail, session.Client{UserAgent: "c"})
	if err != nil {
		t.Fatal(err)
	}

	list, kept, err := f.svc.ListSessions(ctx, f.ann, cur)
	if err != nil || !kept || len(list) != 2 {
		t.Fatalf("ListSessions = %d sessions, kept %v, %v", len(list), kept, err)
	}
	// Bob's session is not ann's, whatever id is asked for.
	bobs, _, _ := f.svc.ListSessions(ctx, f.bob, other)
	if err := f.svc.EndSession(ctx, f.ann, bobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if bobs, _, _ = f.svc.ListSessions(ctx, f.bob, other); len(bobs) != 1 {
		t.Errorf("bob has %d sessions after ann ended his id, want 1", len(bobs))
	}
	if err := f.svc.EndOtherSessions(ctx, f.ann, "not-a-session"); !errors.Is(err, profile.ErrNoCurrentSession) {
		t.Errorf("EndOtherSessions with a stranger's token = %v, want ErrNoCurrentSession", err)
	}
	if list, _, _ = f.svc.ListSessions(ctx, f.ann, cur); len(list) != 2 {
		t.Errorf("ann has %d sessions after a refused end, want 2", len(list))
	}
	if err := f.svc.EndOtherSessions(ctx, f.ann, cur); err != nil {
		t.Fatal(err)
	}
	if list, _, _ = f.svc.ListSessions(ctx, f.ann, cur); len(list) != 1 || !list[0].Current {
		t.Errorf("ann's sessions after ending the others = %+v", list)
	}
}

func TestSessionsWithoutSignIn(t *testing.T) {
	f := newFixture(t, false)
	ctx := context.Background()
	if list, kept, err := f.svc.ListSessions(ctx, f.ann, ""); err != nil || kept || len(list) != 0 {
		t.Errorf("ListSessions = %v, kept %v, %v", list, kept, err)
	}
	if err := f.svc.EndSession(ctx, f.ann, "x"); !errors.Is(err, profile.ErrNoSessions) {
		t.Errorf("EndSession = %v, want ErrNoSessions", err)
	}
	if err := f.svc.EndOtherSessions(ctx, f.ann, "x"); !errors.Is(err, profile.ErrNoSessions) {
		t.Errorf("EndOtherSessions = %v, want ErrNoSessions", err)
	}
}

func TestKeysAndGrantsAreTheCallers(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	secret, err := f.svc.CreateKey(ctx, f.ann, "Laptop", "90d")
	if err != nil || secret == "" {
		t.Fatalf("CreateKey = %q, %v", secret, err)
	}
	if _, err := f.svc.CreateKey(ctx, f.ann, "Laptop", "forever"); !errors.Is(err, profile.ErrKeyInvalid) {
		t.Errorf("an unknown lifetime = %v, want ErrKeyInvalid", err)
	}
	keys, err := f.svc.ListKeys(ctx, f.ann)
	if err != nil || len(keys) != 1 {
		t.Fatalf("ListKeys = %v, %v", keys, err)
	}
	if bobs, _ := f.svc.ListKeys(ctx, f.bob); len(bobs) != 0 {
		t.Errorf("bob sees %d of ann's keys", len(bobs))
	}
	if err := f.svc.RevokeKey(ctx, f.bob, keys[0].ID); !errors.Is(err, profile.ErrKeyNotFound) {
		t.Errorf("bob revoking ann's key = %v, want ErrKeyNotFound", err)
	}
	if err := f.svc.RevokeKey(ctx, f.ann, keys[0].ID); err != nil {
		t.Errorf("ann revoking her key = %v", err)
	}
	if err := f.svc.RevokeGrant(ctx, f.ann, 99); !errors.Is(err, profile.ErrGrantNotFound) {
		t.Errorf("RevokeGrant of nothing = %v, want ErrGrantNotFound", err)
	}
	if g, err := f.svc.ListGrants(ctx, f.ann); err != nil || len(g) != 0 {
		t.Errorf("ListGrants = %v, %v", g, err)
	}
}

// withAvatars gives the profiles of the emails an avatar key, which only the
// avatar job sets in production.
type withAvatars struct {
	profile.Profiles
	keys map[string]string
}

func (w withAvatars) ByAddress(ctx context.Context, address string) (profile.Profile, error) {
	p, err := w.Profiles.ByAddress(ctx, address)
	p.Avatar = w.keys[p.Email]
	return p, err
}

// OpenAvatar takes an address and no relation: the service works the relation
// out from the Caller, so there is no subject for a caller to forge, and what
// is read is the avatar of the profile the address names.
func TestOpenAvatarReadsTheAvatarOfTheAddressedProfile(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	annKey, bobKey := "people/ann/avatar/v1.webp", "people/bob/avatar/v1.webp"
	for k, v := range map[string]string{annKey: "ann's picture", bobKey: "bob's picture"} {
		if err := f.media.Put(ctx, k, []byte(v), "image/webp"); err != nil {
			t.Fatal(err)
		}
	}
	annP, _ := f.svc.Own(ctx, f.ann)
	bobP, _ := f.svc.Own(ctx, f.bob)
	loneP, _ := f.svc.Own(ctx, f.lone)
	svc, err := profile.New(profile.Deps{
		Profiles:   withAvatars{f.profiles, map[string]string{annEmail: annKey, bobEmail: bobKey}},
		Households: f.households, Keys: f.keys, Grants: f.grants, Avatars: f.avatars,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Whoever asks, a profile's address gives that profile's picture, never the
	// asker's own, and a stranger in another household sees it as Other.
	for _, c := range []profile.Caller{f.ann, f.bob, f.cy, f.lone} {
		b, err := svc.OpenAvatar(ctx, c, annP.Address)
		if err != nil || string(b) != "ann's picture" {
			t.Errorf("%s opening ann's avatar = %q, %v", c.Person, b, err)
		}
	}
	if b, err := svc.OpenAvatar(ctx, f.ann, bobP.Address); err != nil || string(b) != "bob's picture" {
		t.Errorf("ann opening bob's avatar = %q, %v", b, err)
	}
	if _, err := svc.OpenAvatar(ctx, f.ann, loneP.Address); !errors.Is(err, profile.ErrNotFound) {
		t.Errorf("a profile with no avatar = %v, want ErrNotFound", err)
	}
	if _, err := svc.OpenAvatar(ctx, f.ann, "AAAAAAAAAAAAAAAAAAAAAA"); !errors.Is(err, profile.ErrNotFound) {
		t.Errorf("a guessed address = %v, want ErrNotFound", err)
	}
	// The visibility rule itself is profile.Visible's; a relation it does not
	// know sees no avatar.
	if got := profile.AvatarKey(profile.Subject{Profile: profile.Profile{Avatar: annKey}, Relation: "unknown"}); got != "" {
		t.Errorf("AvatarKey for an unknown relation = %q, want none", got)
	}
}
