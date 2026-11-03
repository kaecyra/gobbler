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
	short, err := f.keys.CreatePersonal(ctx, "ann@example.com", "short", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := f.keys.CreatePersonal(ctx, "ann@example.com", "gone", 0)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := f.keys.ListPersonal(ctx, "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
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
	secret, err := f.keys.CreatePersonal(ctx, "ann@example.com", "k", 0)
	if err != nil {
		t.Fatal(err)
	}
	used := func() time.Time {
		ks, err := f.keys.ListPersonal(ctx, "ann@example.com")
		if err != nil {
			t.Fatal(err)
		}
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

// personalKey makes a personal key and looks it up as /mcp does.
func personalKey(t *testing.T, f *fixture, email, name string) apikey.Principal {
	t.Helper()
	secret, err := f.keys.CreatePersonal(context.Background(), email, name, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.keys.Lookup(context.Background(), secret)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// householdKey is personalKey for a household key.
func householdKey(t *testing.T, f *fixture, householdID int64, by, name string) apikey.Principal {
	t.Helper()
	secret, err := f.keys.CreateHousehold(context.Background(), householdID, by, name, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.keys.Lookup(context.Background(), secret)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A write is logged with the key as it was when its secret was looked up, so a
// key revoked while the request ran still has its write recorded, and the row
// survives the key (ADR-00031 obligation 5).
func TestRecordWriteSurvivesRevocationOfTheKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h := f.join("Smiths", "ann@example.com", "bob@example.com")
	p := personalKey(t, f, "ann@example.com", "k")
	if err := f.keys.RecordWrite(ctx, p, h.ID, "", "x"); err == nil {
		t.Error("a write with no tool was recorded")
	}
	if err := f.keys.RecordWrite(ctx, apikey.Principal{KeyID: p.KeyID, Email: p.Email}, h.ID, "t", "x"); err == nil {
		t.Error("a write with no key snapshot was recorded")
	}
	// Revoked between the lookup and the record.
	if err := f.keys.RevokePersonal(ctx, "ann@example.com", p.KeyID); err != nil {
		t.Fatal(err)
	}
	if err := f.keys.RecordWrite(ctx, p, h.ID, "add_manual_item", "list 3"); err != nil {
		t.Fatalf("the write of a key revoked mid-request: %v", err)
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
	old := personalKey(t, f, "ann@example.com", "old")
	if err := f.keys.RecordWrite(ctx, old, h.ID, "add_manual_item", "list 3"); err != nil {
		t.Fatal(err)
	}
	if err := f.keys.RevokePersonal(ctx, "ann@example.com", old.KeyID); err != nil {
		t.Fatal(err)
	}
	fresh := personalKey(t, f, "bob@example.com", "new")
	if fresh.KeyID != old.KeyID {
		t.Fatalf("the new key has id %d, not the revoked key's %d; this test no longer exercises id reuse", fresh.KeyID, old.KeyID)
	}
	if ws, err := f.keys.Writes(ctx, fresh); err != nil || len(ws) != 0 {
		t.Errorf("the new key lists %+v, %v", ws, err)
	}
	if ws, err := f.keys.Writes(ctx, old); err != nil || len(ws) != 1 {
		t.Errorf("the revoked key lists %+v, %v; want its one write", ws, err)
	}
}

// A household key's writes are listed under the household owner, and a key of
// another household given the revoked key's id does not see them.
func TestHouseholdKeyWritesAreListedAndSurviveIDReuse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	smiths := f.join("Smiths", "ann@example.com", "bob@example.com")
	joneses, err := f.house.Create(ctx, "Joneses", "jo@example.com")
	if err != nil {
		t.Fatal(err)
	}
	old := householdKey(t, f, smiths.ID, "ann@example.com", "kitchen")
	if err := f.keys.RecordWrite(ctx, old, smiths.ID, "check_shopping_item", "list 1"); err != nil {
		t.Fatal(err)
	}
	ws, err := f.keys.Writes(ctx, old)
	if err != nil || len(ws) != 1 || ws[0].Tool != "check_shopping_item" || ws[0].HouseholdID != smiths.ID {
		t.Fatalf("the household key lists %+v, %v", ws, err)
	}
	if err := f.keys.RevokeHousehold(ctx, smiths.ID, old.KeyID); err != nil {
		t.Fatal(err)
	}
	fresh := householdKey(t, f, joneses.ID, "jo@example.com", "garage")
	if fresh.KeyID != old.KeyID {
		t.Fatalf("the new key has id %d, not the revoked key's %d; this test no longer exercises id reuse", fresh.KeyID, old.KeyID)
	}
	if ws, err := f.keys.Writes(ctx, fresh); err != nil || len(ws) != 0 {
		t.Errorf("the other household's key lists %+v, %v", ws, err)
	}
	if ws, err := f.keys.Writes(ctx, old); err != nil || len(ws) != 1 {
		t.Errorf("the revoked household key lists %+v, %v; want its one write", ws, err)
	}
}

// Deleting a household deletes its history (ADR-00018): the key write log goes
// with it by the schema's cascade.
func TestDeletingAHouseholdDeletesItsKeyWrites(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h, err := f.house.Create(ctx, "Solo", "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	p := personalKey(t, f, "ann@example.com", "k")
	if err := f.keys.RecordWrite(ctx, p, h.ID, "add_manual_item", "list 1"); err != nil {
		t.Fatal(err)
	}
	if err := f.house.Delete(ctx, h.ID, "ann@example.com", "Solo"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.d.QueryRowContext(ctx, `SELECT count(*) FROM api_key_writes`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d key write rows survived the household (%v)", n, err)
	}
}
