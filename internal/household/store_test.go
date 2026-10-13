package household

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

type fixture struct {
	t   *testing.T
	s   *Store
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, config.Config{Server: config.Server{DataDir: t.TempDir()}})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, db.Migrations()); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	f := &fixture{t: t, s: New(d), now: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
	f.s.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) create(name, email string) Household {
	f.t.Helper()
	h, err := f.s.Create(context.Background(), name, email)
	if err != nil {
		f.t.Fatalf("Create(%q, %q): %v", name, email, err)
	}
	return h
}

func (f *fixture) invite(in Invite) int64 {
	f.t.Helper()
	id, err := f.s.Invite(context.Background(), in)
	if err != nil {
		f.t.Fatalf("Invite(%+v): %v", in, err)
	}
	return id
}

func TestForEmailFindsOnlyOwnHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create("Alpha", "ann@example.com")
	b := f.create("Beta", "bob@example.com")

	got, err := f.s.ForEmail(ctx, "  ANN@example.com ")
	if err != nil || got.ID != a.ID || got.Name != "Alpha" {
		t.Fatalf("ForEmail(ann) = %+v, %v; want household %d", got, err, a.ID)
	}
	if got, _ := f.s.ForEmail(ctx, "bob@example.com"); got.ID != b.ID {
		t.Errorf("ForEmail(bob) = %d, want %d", got.ID, b.ID)
	}
	if _, err := f.s.ForEmail(ctx, "stranger@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ForEmail(stranger) = %v, want ErrNotFound", err)
	}
	if _, err := f.s.ForEmail(ctx, "not an email"); !errors.Is(err, ErrInvalid) {
		t.Errorf("ForEmail(garbage) = %v, want ErrInvalid", err)
	}
}

func TestOneHouseholdPerEmail(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := f.create("Alpha", "ann@example.com")

	cases := []struct {
		name string
		do   func() error
	}{
		{"create a second household", func() error { _, err := f.s.Create(ctx, "Other", "ANN@example.com"); return err }},
		{"join another by invitation", func() error {
			other := f.create("Beta", "bob@example.com")
			id := f.invite(Invite{Kind: KindJoin, Email: "ann@example.com", HouseholdID: other.ID, IssuedBy: "bob@example.com"})
			_, err := f.s.Accept(ctx, id, "ann@example.com", "")
			return err
		}},
		{"start a household by invitation", func() error {
			id := f.invite(Invite{Kind: KindHousehold, Email: "ann@example.com", IssuedBy: "op@example.com"})
			_, err := f.s.Accept(ctx, id, "ann@example.com", "Third")
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.do(); !errors.Is(err, ErrAlreadyMember) {
				t.Fatalf("err = %v, want ErrAlreadyMember", err)
			}
			got, err := f.s.ForEmail(ctx, "ann@example.com")
			if err != nil || got.ID != first.ID {
				t.Fatalf("after refusal ann is in %+v, %v; want household %d", got, err, first.ID)
			}
		})
	}
}

func TestRefusedAcceptLeavesInvitationUsable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.create("Alpha", "ann@example.com")
	target := f.create("Beta", "bob@example.com")
	id := f.invite(Invite{Kind: KindJoin, Email: "ann@example.com", HouseholdID: target.ID, IssuedBy: "bob@example.com"})
	if _, err := f.s.Accept(ctx, id, "ann@example.com", ""); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("first accept = %v, want ErrAlreadyMember", err)
	}
	// The claim was rolled back: the invitation is not spent by a refusal.
	if _, err := f.s.Accept(ctx, id, "ann@example.com", ""); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("second accept = %v, want ErrAlreadyMember again, not ErrUsed", err)
	}
}

func TestAcceptJoinAddsMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h := f.create("Alpha", "ann@example.com")
	id := f.invite(Invite{Kind: KindJoin, Email: "Cy@Example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})

	got, err := f.s.Accept(ctx, id, "cy@example.com", "ignored")
	if err != nil || got.ID != h.ID || got.Name != "Alpha" {
		t.Fatalf("Accept = %+v, %v; want household %d", got, err, h.ID)
	}
	members, err := f.s.Members(ctx, h.ID)
	if err != nil || len(members) != 2 || members[1] != "cy@example.com" {
		t.Fatalf("Members = %v, %v", members, err)
	}
}

func TestAcceptHouseholdInvitationCreatesHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.invite(Invite{Kind: KindHousehold, Email: "new@example.com", IssuedBy: "op@example.com"})

	if _, err := f.s.Accept(ctx, id, "new@example.com", "  "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank name = %v, want ErrInvalid", err)
	}
	got, err := f.s.Accept(ctx, id, "new@example.com", "Newcomers")
	if err != nil || got.Name != "Newcomers" {
		t.Fatalf("Accept = %+v, %v", got, err)
	}
	if mine, err := f.s.ForEmail(ctx, "new@example.com"); err != nil || mine.ID != got.ID {
		t.Fatalf("ForEmail = %+v, %v; want %d", mine, err, got.ID)
	}
}

func TestInvitationLifecycle(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		setup func(f *fixture, id int64)
		who   string
		want  error
	}{
		{"valid", func(*fixture, int64) {}, "cy@example.com", nil},
		{"expired", func(f *fixture, _ int64) { f.now = f.now.Add(DefaultInvitationTTL + time.Second) }, "cy@example.com", ErrExpired},
		{"expires exactly at the deadline", func(f *fixture, _ int64) { f.now = f.now.Add(DefaultInvitationTTL) }, "cy@example.com", ErrExpired},
		{"used", func(f *fixture, id int64) {
			if _, err := f.s.Accept(ctx, id, "cy@example.com", ""); err != nil {
				t.Fatal(err)
			}
		}, "cy@example.com", ErrUsed},
		{"revoked", func(f *fixture, id int64) {
			if err := f.s.Revoke(ctx, id, "ann@example.com"); err != nil {
				t.Fatal(err)
			}
		}, "cy@example.com", ErrRevoked},
		{"addressed to someone else", func(*fixture, int64) {}, "dee@example.com", ErrNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			h := f.create("Alpha", "ann@example.com")
			id := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})
			c.setup(f, id)
			_, err := f.s.Accept(ctx, id, c.who, "")
			if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("Accept = %v, want %v", err, c.want)
			}
			if c.want != nil && !errors.Is(c.want, ErrUsed) { // a used invitation made its first accepter a member
				if _, err := f.s.ForEmail(ctx, c.who); !errors.Is(err, ErrNotFound) {
					t.Errorf("a refused accept made %s a member: %v", c.who, err)
				}
			}
		})
	}
}

func TestMissingInvitationIsNotFound(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Accept(context.Background(), 999, "cy@example.com", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Accept(missing) = %v, want ErrNotFound", err)
	}
}

func TestRevoke(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h := f.create("Alpha", "ann@example.com")
	f.create("Beta", "bob@example.com")
	id := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})

	if err := f.s.Revoke(ctx, id, "bob@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoke by a non-issuer = %v, want ErrNotFound", err)
	}
	if err := f.s.Revoke(ctx, id, "ann@example.com"); err != nil {
		t.Fatalf("revoke by issuer: %v", err)
	}
	if err := f.s.Revoke(ctx, id, "ann@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revoke = %v, want ErrNotFound", err)
	}

	used := f.invite(Invite{Kind: KindJoin, Email: "dee@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})
	if _, err := f.s.Accept(ctx, used, "dee@example.com", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Revoke(ctx, used, "ann@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking an accepted invitation = %v, want ErrNotFound", err)
	}
}

func TestInviteValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.create("Alpha", "ann@example.com")
	b := f.create("Beta", "bob@example.com")

	cases := []struct {
		name string
		in   Invite
		want error
	}{
		{"join by a non-member", Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: a.ID, IssuedBy: "stranger@example.com"}, ErrNotFound},
		{"join into another household", Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: b.ID, IssuedBy: "ann@example.com"}, ErrNotFound},
		{"join without a household", Invite{Kind: KindJoin, Email: "cy@example.com", IssuedBy: "ann@example.com"}, ErrInvalid},
		{"household kind naming a household", Invite{Kind: KindHousehold, Email: "cy@example.com", HouseholdID: a.ID, IssuedBy: "op@example.com"}, ErrInvalid},
		{"unknown kind", Invite{Kind: "admin", Email: "cy@example.com", IssuedBy: "op@example.com"}, ErrInvalid},
		{"bad invitee", Invite{Kind: KindHousehold, Email: "nope", IssuedBy: "op@example.com"}, ErrInvalid},
		{"negative lifetime", Invite{Kind: KindHousehold, Email: "cy@example.com", IssuedBy: "op@example.com", TTL: -time.Hour}, ErrInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := f.s.Invite(ctx, c.in); !errors.Is(err, c.want) {
				t.Fatalf("Invite = %v, want %v", err, c.want)
			}
		})
	}
}

func TestCustomLifetime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	id := f.invite(Invite{Kind: KindHousehold, Email: "cy@example.com", IssuedBy: "op@example.com", TTL: time.Hour})
	f.now = f.now.Add(2 * time.Hour)
	if _, err := f.s.Accept(ctx, id, "cy@example.com", "X"); !errors.Is(err, ErrExpired) {
		t.Fatalf("Accept after custom ttl = %v, want ErrExpired", err)
	}
}

func TestSchemaRefusesSecondMembershipDirectly(t *testing.T) {
	f := newFixture(t)
	a := f.create("Alpha", "ann@example.com")
	b := f.create("Beta", "bob@example.com")
	_, err := f.s.db.ExecContext(context.Background(),
		`INSERT INTO household_members (household_id, email, joined_at) VALUES (?, 'ann@example.com', 'x')`, b.ID)
	if err == nil {
		t.Fatalf("schema let ann join household %d as well as %d", b.ID, a.ID)
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for range 2 {
		if err := f.s.Seed(ctx, "Dev", "dev@localhost"); err != nil {
			t.Fatalf("Seed: %v", err)
		}
	}
	h, err := f.s.ForEmail(ctx, "dev@localhost")
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := f.s.Members(ctx, h.ID); len(m) != 1 {
		t.Fatalf("members = %v", m)
	}
}

func TestContext(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("empty context has a household")
	}
	h := Household{ID: 3, Name: "x"}
	if got, ok := FromContext(WithContext(context.Background(), h)); !ok || got != h {
		t.Fatalf("FromContext = %+v, %v", got, ok)
	}
}
