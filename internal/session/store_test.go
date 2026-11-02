package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

const lifetime = 30 * 24 * time.Hour

type fixture struct {
	t   *testing.T
	d   *sql.DB
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
	s, err := New(d, lifetime)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, d: d, s: s, now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	f.s.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) create(email string) string {
	f.t.Helper()
	token, err := f.s.Create(context.Background(), email)
	if err != nil {
		f.t.Fatalf("Create(%q): %v", email, err)
	}
	return token
}

func (f *fixture) lookup(token string) (string, error) {
	return f.s.Lookup(context.Background(), token)
}

// row reads the stored times of the session the token names.
func (f *fixture) row(token string) (created, lastUsed, expires string, ok bool) {
	f.t.Helper()
	err := f.d.QueryRowContext(context.Background(),
		`SELECT created_at, last_used_at, expires_at FROM sessions WHERE token_hash = ?`, hash(token)).
		Scan(&created, &lastUsed, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", false
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return created, lastUsed, expires, true
}

func (f *fixture) count() int {
	f.t.Helper()
	var n int
	if err := f.d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestCreateThenLookup(t *testing.T) {
	f := newFixture(t)
	token := f.create("  Ann@Example.com ")
	if b, err := encoding.DecodeString(token); err != nil || len(b) != tokenBytes {
		t.Fatalf("token %q is not %d base64url bytes: %v", token, tokenBytes, err)
	}
	if strings.ContainsAny(token, "=+/") {
		t.Errorf("token %q is not unpadded base64url", token)
	}
	email, err := f.lookup(token)
	if err != nil || email != "ann@example.com" {
		t.Fatalf("Lookup = %q, %v; want ann@example.com", email, err)
	}
	if other := f.create("ann@example.com"); other == token {
		t.Error("two sessions got the same token")
	}
}

func TestCreateRefusesBlankEmail(t *testing.T) {
	f := newFixture(t)
	if _, err := f.s.Create(context.Background(), "   "); err == nil {
		t.Fatal("Create(blank) succeeded")
	}
	if n := f.count(); n != 0 {
		t.Errorf("sessions = %d, want none", n)
	}
}

func TestCreateFailsWhenRandomSourceFails(t *testing.T) {
	f := newFixture(t)
	f.s.rand = iotest.ErrReader(errors.New("entropy gone"))
	if _, err := f.s.Create(context.Background(), "ann@example.com"); err == nil || !strings.Contains(err.Error(), "entropy gone") {
		t.Fatalf("Create = %v, want the random source's error", err)
	}
	if n := f.count(); n != 0 {
		t.Errorf("sessions = %d, want none", n)
	}
}

// The database holds the token's SHA-256 and nothing from which the cookie
// could be rebuilt (ADR-00030, obligation 2).
func TestDatabaseHoldsOnlyTheTokenHash(t *testing.T) {
	f := newFixture(t)
	token := f.create("ann@example.com")
	raw, err := encoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(token))

	var stored []byte
	if err := f.d.QueryRowContext(context.Background(), `SELECT token_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(stored) != hex.EncodeToString(want[:]) {
		t.Errorf("token_hash = %x, want SHA-256 of the token %x", stored, want)
	}

	rows, err := f.d.QueryContext(context.Background(), `SELECT * FROM sessions`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rows.Close() })
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			var text string
			switch v := v.(type) {
			case []byte:
				text = string(v) + " " + hex.EncodeToString(v)
			case string:
				text = v
			}
			for _, leak := range []string{token, string(raw), hex.EncodeToString(raw)} {
				if strings.Contains(text, leak) {
					t.Errorf("column %s holds the token", cols[i])
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestLookupRefuses(t *testing.T) {
	f := newFixture(t)
	expired := f.create("bob@example.com")
	deleted := f.create("cy@example.com")
	if err := f.s.Delete(context.Background(), deleted); err != nil {
		t.Fatal(err)
	}
	// Ann signs in a second before Bob's session ends, so hers is the one
	// live session when the clock reaches his expiry.
	f.now = f.now.Add(lifetime - time.Second)
	live := f.create("ann@example.com")
	f.now = f.now.Add(time.Second)

	never := encoding.EncodeToString(make([]byte, tokenBytes))
	for _, tt := range []struct {
		name, token string
	}{
		{"expired at exactly its expiry", expired},
		{"deleted", deleted},
		{"never issued", never},
		{"empty", ""},
		{"not base64url", "not a token!"},
		{"padded", never + "="},
		{"too short", encoding.EncodeToString(make([]byte, tokenBytes-1))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			email, err := f.lookup(tt.token)
			if !errors.Is(err, ErrNoSession) {
				t.Fatalf("Lookup = %q, %v; want ErrNoSession", email, err)
			}
			if tt.token != "" && strings.Contains(err.Error(), tt.token) {
				t.Error("the error contains the token")
			}
		})
	}
	if email, err := f.lookup(live); err != nil || email != "ann@example.com" {
		t.Errorf("Lookup(live) = %q, %v", email, err)
	}
}

func TestLookupDeletesAnExpiredSession(t *testing.T) {
	f := newFixture(t)
	token := f.create("ann@example.com")
	f.now = f.now.Add(lifetime + time.Hour)
	if _, err := f.lookup(token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("Lookup = %v, want ErrNoSession", err)
	}
	if _, _, _, ok := f.row(token); ok {
		t.Error("expired session row survived its lookup")
	}
}

// A session lasts a fixed time from sign-in (ADR-00030): use moves
// last_used_at and never expires_at.
func TestLookupDoesNotExtendTheSession(t *testing.T) {
	f := newFixture(t)
	start := f.now
	token := f.create("ann@example.com")
	created, lastUsed, expires, _ := f.row(token)
	if created != lastUsed || expires != start.Add(lifetime).Format(timeLayout) {
		t.Fatalf("new session times = %s, %s, %s", created, lastUsed, expires)
	}

	f.now = start.Add(lifetime - time.Minute)
	if _, err := f.lookup(token); err != nil {
		t.Fatal(err)
	}
	c2, l2, e2, _ := f.row(token)
	if c2 != created || e2 != expires {
		t.Errorf("lookup moved created_at or expires_at: %s, %s; want %s, %s", c2, e2, created, expires)
	}
	if l2 != f.now.Format(timeLayout) {
		t.Errorf("last_used_at = %s, want %s", l2, f.now.Format(timeLayout))
	}

	f.now = start.Add(lifetime)
	if _, err := f.lookup(token); !errors.Is(err, ErrNoSession) {
		t.Errorf("Lookup at the original expiry = %v, want ErrNoSession", err)
	}
}

func TestDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.create("ann@example.com")
	other := f.create("ann@example.com")
	if err := f.s.Delete(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := f.lookup(token); !errors.Is(err, ErrNoSession) {
		t.Errorf("Lookup after Delete = %v, want ErrNoSession", err)
	}
	if email, err := f.lookup(other); err != nil || email != "ann@example.com" {
		t.Errorf("Delete ended another session of the same email: %q, %v", email, err)
	}
	for _, tok := range []string{token, "never issued", ""} {
		if err := f.s.Delete(ctx, tok); err != nil {
			t.Errorf("Delete(%q) of no session = %v, want nil", tok, err)
		}
	}
}

func TestDeleteForEmail(t *testing.T) {
	f := newFixture(t)
	ann1 := f.create("ann@example.com")
	ann2 := f.create("ann@example.com")
	bob := f.create("bob@example.com")

	n, err := f.s.DeleteForEmail(context.Background(), " ANN@example.com")
	if err != nil || n != 2 {
		t.Fatalf("DeleteForEmail = %d, %v; want 2", n, err)
	}
	for _, tok := range []string{ann1, ann2} {
		if _, err := f.lookup(tok); !errors.Is(err, ErrNoSession) {
			t.Errorf("Ann's session survived: %v", err)
		}
	}
	if email, err := f.lookup(bob); err != nil || email != "bob@example.com" {
		t.Errorf("Bob's session = %q, %v; want untouched", email, err)
	}
	if n, err := f.s.DeleteForEmail(context.Background(), "nobody@example.com"); err != nil || n != 0 {
		t.Errorf("DeleteForEmail(nobody) = %d, %v; want 0, nil", n, err)
	}
}

func TestDeleteExpired(t *testing.T) {
	f := newFixture(t)
	f.create("ann@example.com")
	f.create("bob@example.com")
	f.now = f.now.Add(time.Hour)
	fresh := f.create("cy@example.com")
	f.now = f.now.Add(lifetime - time.Hour)

	n, err := f.s.DeleteExpired(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("DeleteExpired = %d, %v; want 2", n, err)
	}
	if n := f.count(); n != 1 {
		t.Errorf("sessions left = %d, want 1", n)
	}
	if email, err := f.lookup(fresh); err != nil || email != "cy@example.com" {
		t.Errorf("unexpired session = %q, %v", email, err)
	}
}

func TestNewRefusesNonPositiveLifetime(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Hour} {
		if _, err := New(nil, d); err == nil {
			t.Errorf("New(lifetime %s) succeeded", d)
		}
	}
}

// Lookup writes last_used_at at most once per touchEvery, so every private
// request does not queue on SQLite's single writer.
func TestLookupTouchesAtMostOncePerInterval(t *testing.T) {
	f := newFixture(t)
	start := f.now
	token := f.create("ann@example.com")

	f.now = start.Add(touchEvery - time.Second)
	if _, err := f.lookup(token); err != nil {
		t.Fatal(err)
	}
	if _, l, _, _ := f.row(token); l != start.Format(timeLayout) {
		t.Errorf("last_used_at moved within touchEvery: %s, want %s", l, start.Format(timeLayout))
	}

	f.now = start.Add(touchEvery)
	if _, err := f.lookup(token); err != nil {
		t.Fatal(err)
	}
	if _, l, _, _ := f.row(token); l != f.now.Format(timeLayout) {
		t.Errorf("last_used_at = %s after touchEvery, want %s", l, f.now.Format(timeLayout))
	}
}

func TestDeleteForEmailRefusesAnEmptyEmail(t *testing.T) {
	f := newFixture(t)
	f.create("ann@example.com")
	for _, e := range []string{"", "   "} {
		if _, err := f.s.DeleteForEmail(context.Background(), e); err == nil {
			t.Errorf("DeleteForEmail(%q) = nil error, want one", e)
		}
	}
	if n := f.count(); n != 1 {
		t.Errorf("sessions left = %d, want 1", n)
	}
}
