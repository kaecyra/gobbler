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
		media: ms,
		ann:   profile.Caller{Person: annEmail}, bob: profile.Caller{Person: bobEmail},
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
		"OpenAvatar": func(c profile.Caller) error {
			_, err := f.svc.OpenAvatar(ctx, c, profile.Subject{Viewer: c.Person, Relation: profile.Self})
			return err
		},
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

type failingHouseholds struct{ err error }

func (f failingHouseholds) ForEmail(context.Context, string) (household.Household, error) {
	return household.Household{}, f.err
}

// A household lookup that fails is a failure, not "not the same household": it
// must not quietly decide what a viewer may see.
func TestSubjectReportsAFailingHouseholdLookup(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	boom := errors.New("database is locked")
	svc, err := profile.New(profile.Deps{
		Profiles: failingProfiles{}, Households: failingHouseholds{boom},
		Keys: apikey.New(nil), Grants: oauth.New(nil), Avatars: failingAvatars{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Subject(ctx, f.bob, "addr"); !errors.Is(err, boom) {
		t.Errorf("Subject error = %v, want the lookup's error", err)
	}
	// Self needs no lookup, so a failing store does not break one's own page.
	if s, err := svc.Subject(ctx, profile.Caller{Person: "Ann@Example.com"}, "addr"); err != nil || s.Relation != profile.Self {
		t.Errorf("own profile with a failing store = %+v, %v", s, err)
	}
}

type failingProfiles struct{ profile.Profiles }

func (failingProfiles) ByAddress(context.Context, string) (profile.Profile, error) {
	return profile.Profile{Email: annEmail, Address: "addr"}, nil
}

type failingAvatars struct{ profile.AvatarUploads }

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

func TestOpenAvatarAsksTheVisibilityRule(t *testing.T) {
	f := newFixture(t, true)
	ctx := context.Background()
	const key = "people/ann/avatar/v1.webp"
	if err := f.media.Put(ctx, key, []byte("picture"), "image/webp"); err != nil {
		t.Fatal(err)
	}
	p := profile.Profile{Email: annEmail, Avatar: key}
	for _, rel := range []profile.Relation{profile.Self, profile.SameHousehold, profile.Other} {
		b, err := f.svc.OpenAvatar(ctx, f.bob, profile.Subject{Profile: p, Viewer: bobEmail, Relation: rel})
		if err != nil || string(b) != "picture" {
			t.Errorf("relation %q = %q, %v", rel, b, err)
		}
	}
	// A relation the rule does not know sees no avatar, even when there is one.
	if _, err := f.svc.OpenAvatar(ctx, f.bob, profile.Subject{Profile: p, Viewer: bobEmail, Relation: "unknown"}); !errors.Is(err, profile.ErrNotFound) {
		t.Errorf("an unknown relation = %v, want ErrNotFound", err)
	}
	// A subject worked out for someone else is not the Caller's to open.
	if _, err := f.svc.OpenAvatar(ctx, f.cy, profile.Subject{Profile: p, Viewer: bobEmail, Relation: profile.Other}); !errors.Is(err, profile.ErrNotMember) {
		t.Errorf("another viewer's subject = %v, want ErrNotMember", err)
	}
}
