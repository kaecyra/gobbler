package household

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRemoveMember(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.create("Alpha", "ann@example.com")
	b := f.create("Beta", "bob@example.com")
	id := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: a.ID, IssuedBy: "ann@example.com"})
	if _, err := f.s.Accept(ctx, id, "cy@example.com", ""); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name         string
		household    int64
		by, target   string
		want         error
		stillMembers []string
	}{
		{"by a member of another household", a.ID, "bob@example.com", "cy@example.com", ErrNotFound, []string{"ann@example.com", "cy@example.com"}},
		{"by someone in no household", a.ID, "stranger@example.com", "cy@example.com", ErrNotFound, []string{"ann@example.com", "cy@example.com"}},
		{"a member of another household", a.ID, "ann@example.com", "bob@example.com", ErrNotFound, []string{"ann@example.com", "cy@example.com"}},
		{"oneself", a.ID, "ann@example.com", "ann@example.com", ErrInvalid, []string{"ann@example.com", "cy@example.com"}},
		{"a fellow member", a.ID, "ann@example.com", "Cy@Example.com", nil, []string{"ann@example.com"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := f.s.Remove(ctx, c.household, c.by, c.target)
			if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("Remove = %v, want %v", err, c.want)
			}
			got, _ := f.s.Members(ctx, a.ID)
			if len(got) != len(c.stillMembers) {
				t.Fatalf("members of Alpha = %v, want %v", got, c.stillMembers)
			}
		})
	}
	if got, _ := f.s.Members(ctx, b.ID); len(got) != 1 {
		t.Errorf("Beta lost a member: %v", got)
	}
	if _, err := f.s.ForEmail(ctx, "cy@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("removed member still belongs to a household: %v", err)
	}
}

func TestLeave(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	h := f.create("Alpha", "ann@example.com")
	id := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})
	if _, err := f.s.Accept(ctx, id, "cy@example.com", ""); err != nil {
		t.Fatal(err)
	}

	if err := f.s.Leave(ctx, "cy@example.com"); err != nil {
		t.Fatalf("Leave(cy): %v", err)
	}
	if _, err := f.s.ForEmail(ctx, "cy@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cy still belongs to a household: %v", err)
	}
	if err := f.s.Leave(ctx, "ann@example.com"); !errors.Is(err, ErrLastMember) {
		t.Fatalf("the last member leaving = %v, want ErrLastMember", err)
	}
	if got, err := f.s.ForEmail(ctx, "ann@example.com"); err != nil || got.ID != h.ID {
		t.Errorf("the refused last member lost the household: %+v, %v", got, err)
	}
	if err := f.s.Leave(ctx, "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Leave by a non-member = %v, want ErrNotFound", err)
	}
	// Having left, a person can take another household.
	other := f.create("Beta", "bob@example.com")
	id = f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: other.ID, IssuedBy: "bob@example.com"})
	if got, err := f.s.Accept(ctx, id, "cy@example.com", ""); err != nil || got.ID != other.ID {
		t.Errorf("accept after leaving = %+v, %v", got, err)
	}
}

func TestDeleteRequiresLastMemberAndTypedName(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	h := f.create("Alpha Kitchen", "ann@example.com")
	id := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})
	if _, err := f.s.Accept(ctx, id, "cy@example.com", ""); err != nil {
		t.Fatal(err)
	}
	f.create("Beta", "bob@example.com")

	if err := f.s.Delete(ctx, h.ID, "ann@example.com", "Alpha Kitchen"); !errors.Is(err, ErrNotLastMember) {
		t.Fatalf("delete with two members = %v, want ErrNotLastMember", err)
	}
	if err := f.s.Remove(ctx, h.ID, "ann@example.com", "cy@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Delete(ctx, h.ID, "bob@example.com", "Alpha Kitchen"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete by a member of another household = %v, want ErrNotFound", err)
	}
	for _, typed := range []string{"", "alpha kitchen", "Alpha"} {
		if err := f.s.Delete(ctx, h.ID, "ann@example.com", typed); !errors.Is(err, ErrConfirmation) {
			t.Errorf("delete typing %q = %v, want ErrConfirmation", typed, err)
		}
	}
	if _, err := f.s.ForEmail(ctx, "ann@example.com"); err != nil {
		t.Fatalf("a refused delete removed the household: %v", err)
	}
	if err := f.s.Delete(ctx, h.ID, "ann@example.com", " Alpha Kitchen "); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := f.s.ForEmail(ctx, "ann@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("household still reachable after delete: %v", err)
	}
	if _, err := f.s.ForEmail(ctx, "bob@example.com"); err != nil {
		t.Errorf("delete reached another household: %v", err)
	}
}

func TestPendingInvitations(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.create("Alpha", "ann@example.com")
	b := f.create("Beta", "bob@example.com")
	keep := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: a.ID, IssuedBy: "ann@example.com"})
	revoked := f.invite(Invite{Kind: KindJoin, Email: "dee@example.com", HouseholdID: a.ID, IssuedBy: "ann@example.com"})
	used := f.invite(Invite{Kind: KindJoin, Email: "eve@example.com", HouseholdID: a.ID, IssuedBy: "ann@example.com"})
	f.invite(Invite{Kind: KindJoin, Email: "fay@example.com", HouseholdID: a.ID, IssuedBy: "ann@example.com", TTL: time.Minute})
	f.invite(Invite{Kind: KindJoin, Email: "gus@example.com", HouseholdID: b.ID, IssuedBy: "bob@example.com"})
	f.invite(Invite{Kind: KindHousehold, Email: "hal@example.com", IssuedBy: "op@example.com"})
	if err := f.s.Revoke(ctx, revoked, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Accept(ctx, used, "eve@example.com", ""); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)

	got, err := f.s.PendingForHousehold(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != keep || got[0].Email != "cy@example.com" || got[0].IssuedBy != "ann@example.com" || got[0].Kind != KindJoin {
		t.Fatalf("pending for Alpha = %+v, want only invitation %d", got, keep)
	}
}

func TestPendingHouseholdInvitationsAreTheIssuers(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	mine := f.invite(Invite{Kind: KindHousehold, Email: "cy@example.com", IssuedBy: "op@example.com"})
	f.invite(Invite{Kind: KindHousehold, Email: "dee@example.com", IssuedBy: "op2@example.com"})
	h := f.create("Alpha", "ann@example.com")
	f.invite(Invite{Kind: KindJoin, Email: "eve@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})

	got, err := f.s.PendingHouseholdInvitations(ctx, "OP@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != mine || got[0].HouseholdID != 0 || got[0].Kind != KindHousehold {
		t.Fatalf("pending = %+v, want only invitation %d", got, mine)
	}
}

func TestInvitationFor(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	h := f.create("Alpha", "ann@example.com")
	id := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})

	got, err := f.s.InvitationFor(ctx, id, "Cy@example.com")
	if err != nil || got.HouseholdName != "Alpha" || got.IssuedBy != "ann@example.com" || got.Kind != KindJoin {
		t.Fatalf("InvitationFor = %+v, %v", got, err)
	}
	if _, err := f.s.InvitationFor(ctx, id, "dee@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("someone else's invitation = %v, want ErrNotFound", err)
	}
	if _, err := f.s.InvitationFor(ctx, 999, "cy@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing invitation = %v, want ErrNotFound", err)
	}
	if err := f.s.Revoke(ctx, id, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.InvitationFor(ctx, id, "cy@example.com"); !errors.Is(err, ErrRevoked) {
		t.Errorf("revoked invitation = %v, want ErrRevoked", err)
	}
	exp := f.invite(Invite{Kind: KindHousehold, Email: "cy@example.com", IssuedBy: "op@example.com"})
	f.now = f.now.Add(DefaultInvitationTTL)
	if _, err := f.s.InvitationFor(ctx, exp, "cy@example.com"); !errors.Is(err, ErrExpired) {
		t.Errorf("expired invitation = %v, want ErrExpired", err)
	}
}
