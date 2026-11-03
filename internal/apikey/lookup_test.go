package apikey_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/apikey"
)

func TestLookupResolvesPersonalAndHouseholdKeys(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h := f.join("Smiths", "ann@example.com", "bob@example.com")
	personal, err := f.keys.CreatePersonal(ctx, "Ann@Example.com", "laptop", 0)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := f.keys.CreateHousehold(ctx, h.ID, "bob@example.com", "kitchen", 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.keys.Lookup(ctx, personal)
	if err != nil || p.Email != "ann@example.com" || p.HouseholdID != 0 || p.KeyID == 0 {
		t.Errorf("personal Lookup = %+v, %v", p, err)
	}
	g, err := f.keys.Lookup(ctx, shared)
	if err != nil || g.Email != "" || g.HouseholdID != h.ID || g.HouseholdName != "Smiths" || g.KeyID == p.KeyID {
		t.Errorf("household Lookup = %+v, %v", g, err)
	}
}

func TestLookupRefusesUnknownRevokedAndExpiredAlike(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	f.keys.SetNow(func() time.Time { return now })
	short, _ := f.keys.CreatePersonal(ctx, "ann@example.com", "short", time.Hour)
	revoked, _ := f.keys.CreatePersonal(ctx, "ann@example.com", "gone", 0)
	keys, _ := f.keys.ListPersonal(ctx, "ann@example.com")
	for _, k := range keys {
		if k.Name == "gone" {
			if err := f.keys.RevokePersonal(ctx, "ann@example.com", k.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := f.keys.Lookup(ctx, short); err != nil {
		t.Fatalf("live key refused: %v", err)
	}
	f.keys.SetNow(func() time.Time { return now.Add(time.Hour) })
	for name, secret := range map[string]string{"unknown": "gob_nope", "revoked": revoked, "expired": short, "empty": ""} {
		if _, err := f.keys.Lookup(ctx, secret); !errors.Is(err, apikey.ErrUnknownKey) {
			t.Errorf("%s: err = %v, want ErrUnknownKey", name, err)
		}
	}
}

func TestLookupStampsLastUseAtMostOncePerMinute(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	f.keys.SetNow(func() time.Time { return now })
	secret, _ := f.keys.CreatePersonal(ctx, "ann@example.com", "k", 0)
	used := func() time.Time {
		ks, _ := f.keys.ListPersonal(ctx, "ann@example.com")
		return ks[0].LastUsedAt
	}
	if _, err := f.keys.Lookup(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if !used().Equal(now) {
		t.Fatalf("last used = %v, want %v", used(), now)
	}
	f.keys.SetNow(func() time.Time { return now.Add(30 * time.Second) })
	f.keys.Lookup(ctx, secret) //nolint:errcheck // asserted through the stamp below
	if !used().Equal(now) {
		t.Errorf("restamped within a minute: %v", used())
	}
	f.keys.SetNow(func() time.Time { return now.Add(2 * time.Minute) })
	f.keys.Lookup(ctx, secret) //nolint:errcheck // asserted through the stamp below
	if !used().Equal(now.Add(2 * time.Minute)) {
		t.Errorf("not restamped after a minute: %v", used())
	}
}

func TestRecordWriteSurvivesRevocationOfTheKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h := f.join("Smiths", "ann@example.com", "bob@example.com")
	secret, _ := f.keys.CreatePersonal(ctx, "ann@example.com", "k", 0)
	p, _ := f.keys.Lookup(ctx, secret)
	if err := f.keys.RecordWrite(ctx, p.KeyID, h.ID, "add_manual_item", "list 3"); err != nil {
		t.Fatal(err)
	}
	if err := f.keys.RecordWrite(ctx, p.KeyID, h.ID, "", "x"); err == nil {
		t.Error("a write with no tool was recorded")
	}
	if err := f.keys.RevokePersonal(ctx, "ann@example.com", p.KeyID); err != nil {
		t.Fatal(err)
	}
	ws, err := f.keys.Writes(ctx, p)
	if err != nil || len(ws) != 1 || ws[0].Tool != "add_manual_item" || ws[0].Target != "list 3" || ws[0].HouseholdID != h.ID {
		t.Errorf("Writes = %+v, %v", ws, err)
	}
}

// SQLite gives a revoked key's id to the next key. That key must not list the
// revoked one's writes, which stay attributed to the key that made them.
func TestAKeyGivenARevokedKeysIDDoesNotSeeItsWrites(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h := f.join("Smiths", "ann@example.com", "bob@example.com")
	secret, _ := f.keys.CreatePersonal(ctx, "ann@example.com", "old", 0)
	old, _ := f.keys.Lookup(ctx, secret)
	if err := f.keys.RecordWrite(ctx, old.KeyID, h.ID, "add_manual_item", "list 3"); err != nil {
		t.Fatal(err)
	}
	if err := f.keys.RevokePersonal(ctx, "ann@example.com", old.KeyID); err != nil {
		t.Fatal(err)
	}
	secret, _ = f.keys.CreatePersonal(ctx, "bob@example.com", "new", 0)
	fresh, _ := f.keys.Lookup(ctx, secret)
	if fresh.KeyID != old.KeyID {
		t.Fatalf("the new key has id %d, not the revoked key's %d; this test no longer exercises id reuse", fresh.KeyID, old.KeyID)
	}
	if ws, err := f.keys.Writes(ctx, fresh); err != nil || len(ws) != 0 {
		t.Errorf("the new key lists %+v, %v", ws, err)
	}
	if ws, err := f.keys.Writes(ctx, old); err != nil || len(ws) != 1 {
		t.Errorf("the revoked key lists %+v, %v; want its one write", ws, err)
	}
	if err := f.keys.RecordWrite(ctx, old.KeyID+1000, h.ID, "t", ""); err == nil {
		t.Error("a write of a key that does not exist was recorded")
	}
}

// Deleting a household deletes its history (ADR-00018): the key write log goes
// with it by the schema's cascade.
func TestDeletingAHouseholdDeletesItsKeyWrites(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	hs := f.house
	h, err := hs.Create(ctx, "Solo", "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := f.keys.CreatePersonal(ctx, "ann@example.com", "k", 0)
	p, _ := f.keys.Lookup(ctx, secret)
	if err := f.keys.RecordWrite(ctx, p.KeyID, h.ID, "add_manual_item", "list 1"); err != nil {
		t.Fatal(err)
	}
	if err := hs.Delete(ctx, h.ID, "ann@example.com", "Solo"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.d.QueryRowContext(ctx, `SELECT count(*) FROM api_key_writes`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d key write rows survived the household (%v)", n, err)
	}
}
