package apikey_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/kaecyra/gobbler/internal/apikey"
	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
	"github.com/kaecyra/gobbler/internal/household"
)

type fixture struct {
	t     *testing.T
	d     *sql.DB
	keys  *apikey.Store
	house *household.Store
}

// newFixture returns a migrated database with a key store and a household
// store whose leave hook is the key store's, as the router wires them.
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
	f := &fixture{t: t, d: d, keys: apikey.New(d), house: household.New(d)}
	f.house.OnMemberLeave(f.keys.RevokeForMember)
	return f
}

// join makes a household with first and adds second to it by invitation.
func (f *fixture) join(name, first, second string) household.Household {
	f.t.Helper()
	ctx := context.Background()
	h, err := f.house.Create(ctx, name, first)
	if err != nil {
		f.t.Fatal(err)
	}
	code, err := f.house.Invite(ctx, household.Invite{Kind: household.KindJoin, Email: second, HouseholdID: h.ID, IssuedBy: first})
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.house.Accept(ctx, code, second, ""); err != nil {
		f.t.Fatal(err)
	}
	return h
}

func (f *fixture) count(where string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.d.QueryRow(`SELECT count(*) FROM api_keys WHERE `+where, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestPersonalKeyIsListedWithoutItsSecretAndStoredOnlyAsAHash(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	secret, err := f.keys.CreatePersonal(ctx, "Ann@Example.com", " laptop ", 30*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "gob_") || len(secret) < 40 {
		t.Fatalf("secret = %q, want a gob_ key of 256 bits", secret)
	}
	keys, err := f.keys.ListPersonal(ctx, "ann@example.com")
	if err != nil || len(keys) != 1 {
		t.Fatalf("ListPersonal = %v, %v", keys, err)
	}
	k := keys[0]
	if k.Name != "laptop" || k.CreatedBy != "ann@example.com" || !strings.HasPrefix(secret, k.Prefix) || k.Prefix == secret {
		t.Errorf("key = %+v for secret %q", k, secret)
	}
	if want := k.CreatedAt.Add(30 * 24 * time.Hour); !k.ExpiresAt.Equal(want) {
		t.Errorf("expires %v, want %v", k.ExpiresAt, want)
	}
	if !k.LastUsedAt.IsZero() {
		t.Errorf("a new key was used at %v", k.LastUsedAt)
	}

	// ADR-00031 obligation 1: the secret is in the creating response and in no
	// column of any row.
	var stored []byte
	if err := f.d.QueryRow(`SELECT token_hash FROM api_keys`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if h := sha256.Sum256([]byte(secret)); !bytes.Equal(stored, h[:]) {
		t.Errorf("stored hash is not the SHA-256 of the secret")
	}
	dump := f.d.QueryRow(`SELECT count(*) FROM api_keys WHERE instr(prefix || name || coalesce(owner_email,'') || created_by, ?) > 0 OR instr(CAST(token_hash AS TEXT), ?) > 0`, secret, secret)
	var leaked int
	if err := dump.Scan(&leaked); err != nil || leaked != 0 {
		t.Errorf("the secret appears in %d stored columns, %v", leaked, err)
	}
}

func TestKeyWithNoExpiryHasNoExpiryAndAnExpiredOneReportsIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.keys.CreatePersonal(ctx, "ann@example.com", "forever", 0); err != nil {
		t.Fatal(err)
	}
	keys, _ := f.keys.ListPersonal(ctx, "ann@example.com")
	if !keys[0].ExpiresAt.IsZero() || keys[0].Expired(time.Now().AddDate(100, 0, 0)) {
		t.Errorf("a key with no expiry expires at %v", keys[0].ExpiresAt)
	}
	short := apikey.Key{ExpiresAt: time.Now().Add(-time.Second)}
	if !short.Expired(time.Now()) {
		t.Error("a key past its expiry is not expired")
	}
}

func TestEverySecretIsDrawnFreshAndARandomSourceFailureIsAnError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, _ := f.keys.CreatePersonal(ctx, "ann@example.com", "a", 0)
	b, _ := f.keys.CreatePersonal(ctx, "ann@example.com", "b", 0)
	if a == b {
		t.Error("two keys have the same secret")
	}
	broken := apikey.NewWithRand(f.d, iotest.ErrReader(errors.New("no entropy")))
	if _, err := broken.CreatePersonal(ctx, "ann@example.com", "c", 0); err == nil {
		t.Error("a failing random source made a key")
	}
	if n := f.count(`1`); n != 2 {
		t.Errorf("%d keys stored, want 2", n)
	}
}

func TestRefusesABlankOrLongNameAndANegativeLifetime(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, name := range []string{"", "   ", strings.Repeat("x", apikey.MaxNameRunes+1)} {
		_, err := f.keys.CreatePersonal(ctx, "ann@example.com", name, 0)
		if reason, ok := apikey.InvalidReason(err); !ok || reason == "" {
			t.Errorf("name %q: err = %v, want an invalid-input reason", name, err)
		}
	}
	if _, err := f.keys.CreatePersonal(ctx, "ann@example.com", "ok", -time.Hour); !errors.Is(err, apikey.ErrInvalid) {
		t.Errorf("negative lifetime: err = %v", err)
	}
	if n := f.count(`1`); n != 0 {
		t.Errorf("%d keys stored after refusals", n)
	}
}

func TestLifetimeChoicesIncludeNoExpiry(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range apikey.Lifetimes {
		seen[l.Value] = true
	}
	for _, want := range []string{apikey.DefaultLifetime, "never"} {
		if !seen[want] {
			t.Errorf("no lifetime choice %q", want)
		}
	}
	never, err := apikey.LifetimeFor("never")
	if err != nil || never.Duration() != 0 {
		t.Errorf("never = %+v, %v", never, err)
	}
	if _, err := apikey.LifetimeFor("forever and ever"); !errors.Is(err, apikey.ErrInvalid) {
		t.Errorf("unknown choice: err = %v", err)
	}
}

// ADR-00031: a person revokes their own key and nobody else's.
func TestRevokeDeletesOnlyTheOwnersKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.keys.CreatePersonal(ctx, "ann@example.com", "mine", 0); err != nil {
		t.Fatal(err)
	}
	keys, _ := f.keys.ListPersonal(ctx, "ann@example.com")
	id := keys[0].ID
	if err := f.keys.RevokePersonal(ctx, "bob@example.com", id); !errors.Is(err, apikey.ErrNotFound) {
		t.Fatalf("another person revoking: err = %v, want ErrNotFound", err)
	}
	if got, _ := f.keys.ListPersonal(ctx, "bob@example.com"); len(got) != 0 {
		t.Errorf("bob lists %v", got)
	}
	if err := f.keys.RevokePersonal(ctx, "ann@example.com", id); err != nil {
		t.Fatal(err)
	}
	if err := f.keys.RevokePersonal(ctx, "ann@example.com", id); !errors.Is(err, apikey.ErrNotFound) {
		t.Errorf("revoking twice: err = %v", err)
	}
}

func TestHouseholdKeyIsMadeByAMemberAndSeenByTheHouseholdOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alpha := f.join("Alpha", "ann@example.com", "abe@example.com")
	beta, err := f.house.Create(ctx, "Beta", "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.keys.CreateHousehold(ctx, alpha.ID, "abe@example.com", "kitchen bot", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.keys.CreateHousehold(ctx, alpha.ID, "bob@example.com", "intruder", 0); !errors.Is(err, apikey.ErrNotFound) {
		t.Errorf("a non-member making a household key: err = %v, want ErrNotFound", err)
	}
	if got, _ := f.keys.ListHousehold(ctx, beta.ID); len(got) != 0 {
		t.Errorf("another household lists %v", got)
	}
	mine, _ := f.keys.ListHousehold(ctx, alpha.ID)
	if len(mine) != 1 || mine[0].CreatedBy != "abe@example.com" {
		t.Fatalf("household keys = %+v", mine)
	}
	if got, _ := f.keys.ListPersonal(ctx, "abe@example.com"); len(got) != 0 {
		t.Errorf("a household key is listed as a personal one: %v", got)
	}
	if err := f.keys.RevokeHousehold(ctx, beta.ID, mine[0].ID); !errors.Is(err, apikey.ErrNotFound) {
		t.Errorf("another household revoking: err = %v, want ErrNotFound", err)
	}
	// Any member, not only the creator, may revoke it (ADR-00031).
	if err := f.keys.RevokeHousehold(ctx, alpha.ID, mine[0].ID); err != nil {
		t.Fatal(err)
	}
}

// ADR-00031 obligation 3, removal.
func TestRemovingAMemberRevokesTheirPersonalKeysAndTheHouseholdKeysTheyMade(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alpha := f.join("Alpha", "ann@example.com", "abe@example.com")
	f.mustPersonal("abe@example.com", "abe laptop")
	f.mustPersonal("ann@example.com", "ann laptop")
	f.mustHousehold(alpha.ID, "abe@example.com", "abe made")
	f.mustHousehold(alpha.ID, "ann@example.com", "ann made")

	if err := f.house.Remove(ctx, alpha.ID, "ann@example.com", "abe@example.com"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`owner_email = 'abe@example.com' OR created_by = 'abe@example.com'`); n != 0 {
		t.Errorf("%d keys of the removed member remain", n)
	}
	if n := f.count(`1`); n != 2 {
		t.Errorf("%d keys remain, want ann's two", n)
	}
}

// ADR-00031 obligation 3, leaving.
func TestLeavingRevokesTheirPersonalKeysAndTheHouseholdKeysTheyMade(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alpha := f.join("Alpha", "ann@example.com", "abe@example.com")
	f.mustPersonal("abe@example.com", "abe laptop")
	f.mustHousehold(alpha.ID, "abe@example.com", "abe made")
	f.mustHousehold(alpha.ID, "ann@example.com", "ann made")

	if err := f.house.Leave(ctx, "abe@example.com"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`owner_email = 'abe@example.com' OR created_by = 'abe@example.com'`); n != 0 {
		t.Errorf("%d keys of the member who left remain", n)
	}
	if n := f.count(`household_id = ?`, alpha.ID); n != 1 {
		t.Errorf("%d household keys remain, want ann's", n)
	}
}

// The revocation is in the removal's transaction: if it fails, the member is
// still a member and keeps their keys.
func TestAFailedRevocationRollsTheRemovalBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alpha := f.join("Alpha", "ann@example.com", "abe@example.com")
	f.mustPersonal("abe@example.com", "abe laptop")
	if _, err := f.d.ExecContext(ctx, `ALTER TABLE api_keys RENAME TO api_keys_gone`); err != nil {
		t.Fatal(err)
	}
	if err := f.house.Remove(ctx, alpha.ID, "ann@example.com", "abe@example.com"); err == nil {
		t.Fatal("Remove succeeded although the keys could not be revoked")
	}
	if members, _ := f.house.Members(ctx, alpha.ID); len(members) != 2 {
		t.Errorf("members = %v, want both kept", members)
	}
}

// ADR-00031 obligation 3, deleting the household.
func TestDeletingAHouseholdRevokesItsHouseholdKeys(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alpha, err := f.house.Create(ctx, "Alpha", "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := f.house.Create(ctx, "Beta", "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	f.mustHousehold(alpha.ID, "ann@example.com", "alpha bot")
	f.mustHousehold(beta.ID, "bob@example.com", "beta bot")
	f.mustPersonal("ann@example.com", "ann laptop")

	if err := f.house.Delete(ctx, alpha.ID, "ann@example.com", "Alpha"); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`household_id = ?`, alpha.ID); n != 0 {
		t.Errorf("%d keys of the deleted household remain", n)
	}
	if n := f.count(`household_id = ?`, beta.ID); n != 1 {
		t.Errorf("another household has %d keys, want 1", n)
	}
}

func TestSchemaRefusesAKeyWithNoOwnerOrTwo(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	h, err := f.house.Create(ctx, "Alpha", "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	for name, ins := range map[string]string{
		"no owner":    `INSERT INTO api_keys (token_hash, prefix, name, created_by, created_at) VALUES (randomblob(32), 'p', 'n', 'a@b.c', 't')`,
		"two owners":  `INSERT INTO api_keys (token_hash, prefix, name, owner_email, household_id, created_by, created_at) VALUES (randomblob(32), 'p', 'n', 'a@b.c', ` + strconv.FormatInt(h.ID, 10) + `, 'a@b.c', 't')`,
		"short hash":  `INSERT INTO api_keys (token_hash, prefix, name, owner_email, created_by, created_at) VALUES (randomblob(16), 'p', 'n', 'a@b.c', 'a@b.c', 't')`,
		"blank name":  `INSERT INTO api_keys (token_hash, prefix, name, owner_email, created_by, created_at) VALUES (randomblob(32), 'p', '', 'a@b.c', 'a@b.c', 't')`,
		"upper email": `INSERT INTO api_keys (token_hash, prefix, name, owner_email, created_by, created_at) VALUES (randomblob(32), 'p', 'n', 'A@b.c', 'a@b.c', 't')`,
	} {
		if _, err := f.d.ExecContext(ctx, ins); err == nil {
			t.Errorf("schema accepted a key with %s", name)
		}
	}
}

func (f *fixture) mustPersonal(email, name string) {
	f.t.Helper()
	if _, err := f.keys.CreatePersonal(context.Background(), email, name, 0); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) mustHousehold(id int64, by, name string) {
	f.t.Helper()
	if _, err := f.keys.CreateHousehold(context.Background(), id, by, name, 0); err != nil {
		f.t.Fatal(err)
	}
}

func TestKeyTextsSayWhenItWasMadeExpiresAndWasUsed(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	k := apikey.Key{CreatedAt: now.Add(-48 * time.Hour)}
	if k.CreatedText() != "2026-11-01" || k.ExpiryText(now) != "No expiry" || k.UsedText() != "Never used" {
		t.Errorf("new key: %q %q %q", k.CreatedText(), k.ExpiryText(now), k.UsedText())
	}
	k.ExpiresAt, k.LastUsedAt = now.Add(24*time.Hour), now.Add(-time.Hour)
	if k.ExpiryText(now) != "Expires 2026-11-04" || k.UsedText() != "Last used 2026-11-03" {
		t.Errorf("live key: %q %q", k.ExpiryText(now), k.UsedText())
	}
	k.ExpiresAt = now.Add(-24 * time.Hour)
	if k.ExpiryText(now) != "Expired 2026-11-02" {
		t.Errorf("expired key: %q", k.ExpiryText(now))
	}
}
