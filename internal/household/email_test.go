package household

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRecordEmailKeepsLastSuccessAndLatestError(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	h := f.create("Alpha", "ann@example.com")
	code := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})

	inv, err := f.s.PendingJoinForSend(ctx, h.ID, code)
	if err != nil || !inv.EmailedAt.IsZero() || inv.EmailError != "" || inv.HouseholdName != "Alpha" {
		t.Fatalf("new invitation = %+v, %v; want never emailed, no error, household Alpha", inv, err)
	}

	if err := f.s.RecordEmail(ctx, code, errors.New("dial: refused")); err != nil {
		t.Fatal(err)
	}
	inv, _ = f.s.PendingJoinForSend(ctx, h.ID, code)
	if !inv.EmailedAt.IsZero() || inv.EmailError != "dial: refused" {
		t.Errorf("after failure = %+v, want error stored and no sent time", inv)
	}

	if err := f.s.RecordEmail(ctx, code, nil); err != nil {
		t.Fatal(err)
	}
	sentAt := f.now
	inv, _ = f.s.PendingJoinForSend(ctx, h.ID, code)
	if !inv.EmailedAt.Equal(sentAt) || inv.EmailError != "" {
		t.Errorf("after success = %+v, want sent at %s and no error", inv, sentAt)
	}

	f.now = f.now.Add(time.Minute)
	if err := f.s.RecordEmail(ctx, code, errors.New("later failure")); err != nil {
		t.Fatal(err)
	}
	inv, _ = f.s.PendingJoinForSend(ctx, h.ID, code)
	if !inv.EmailedAt.Equal(sentAt) || inv.EmailError != "later failure" {
		t.Errorf("after a later failure = %+v, want the earlier sent time kept", inv)
	}
	pending, err := f.s.PendingForHousehold(ctx, h.ID)
	if err != nil || len(pending) != 1 || pending[0].EmailError != "later failure" || !pending[0].EmailedAt.Equal(sentAt) {
		t.Errorf("pending list = %+v, %v; want the status carried", pending, err)
	}
}

func TestRecordEmailBoundsTheStoredError(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	h := f.create("Alpha", "ann@example.com")
	code := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})
	if err := f.s.RecordEmail(ctx, code, errors.New(strings.Repeat("x", 5000))); err != nil {
		t.Fatal(err)
	}
	inv, _ := f.s.PendingJoinForSend(ctx, h.ID, code)
	if len(inv.EmailError) != MaxEmailErrorBytes {
		t.Errorf("stored error is %d bytes, want %d", len(inv.EmailError), MaxEmailErrorBytes)
	}
}

func TestPendingForSendIsScoped(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.create("Alpha", "ann@example.com")
	b := f.create("Beta", "bob@example.com")
	join := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: a.ID, IssuedBy: "ann@example.com"})
	hh := f.invite(Invite{Kind: KindHousehold, Email: "hal@example.com", IssuedBy: "op@example.com"})

	if _, err := f.s.PendingJoinForSend(ctx, b.ID, join); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household's join invitation = %v, want ErrNotFound", err)
	}
	if _, err := f.s.PendingJoinForSend(ctx, a.ID, hh); !errors.Is(err, ErrNotFound) {
		t.Errorf("household invitation through the join lookup = %v, want ErrNotFound", err)
	}
	if _, err := f.s.PendingHouseholdForSend(ctx, "other@example.com", hh); !errors.Is(err, ErrNotFound) {
		t.Errorf("another issuer's household invitation = %v, want ErrNotFound", err)
	}
	if inv, err := f.s.PendingHouseholdForSend(ctx, "OP@example.com", hh); err != nil || inv.Email != "hal@example.com" {
		t.Errorf("issuer's household invitation = %+v, %v", inv, err)
	}
	if err := f.s.Revoke(ctx, join, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PendingJoinForSend(ctx, a.ID, join); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked invitation = %v, want ErrNotFound", err)
	}
	if err := f.s.RecordEmail(ctx, "nosuchcd", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("RecordEmail on unknown code = %v, want ErrNotFound", err)
	}
}
