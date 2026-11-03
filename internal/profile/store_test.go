package profile

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/kaecyra/gobbler/internal/config"
	"github.com/kaecyra/gobbler/internal/db"
)

func newStore(t *testing.T) *Store {
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
	return New(d)
}

func TestEnsureCreatesOnceAndKeepsTheAddress(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	first, err := s.Ensure(ctx, " Ann@Example.com ")
	if err != nil {
		t.Fatal(err)
	}
	if first.Email != "ann@example.com" {
		t.Errorf("Email = %q, want it lower-cased and trimmed", first.Email)
	}
	again, err := s.Ensure(ctx, "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if again.Address != first.Address {
		t.Errorf("a second Ensure changed the address: %q then %q", first.Address, again.Address)
	}
	other, err := s.Ensure(ctx, "bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if other.Address == first.Address {
		t.Error("two people share an address")
	}
}

// ADR-00032 obligation 3: the address is random and does not contain the email.
func TestAddressIsRandomAndHoldsNoEmail(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p, err := s.Ensure(ctx, "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Address) != 22 {
		t.Errorf("address %q is %d characters, want 22", p.Address, len(p.Address))
	}
	for _, part := range []string{"ann", "example", "@"} {
		if strings.Contains(strings.ToLower(p.Address), part) {
			t.Errorf("address %q contains %q", p.Address, part)
		}
	}
}

func TestEnsureReportsAFailingRandomSource(t *testing.T) {
	s := newStore(t)
	s.rand = iotest.ErrReader(errors.New("no entropy"))
	if _, err := s.Ensure(context.Background(), "ann@example.com"); err == nil {
		t.Fatal("Ensure succeeded without a random source")
	}
	if _, err := s.q.GetProfileByEmail(context.Background(), "ann@example.com"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("a profile was stored without an address: %v", err)
	}
}

func TestEnsureRefusesAnEmptyEmail(t *testing.T) {
	if _, err := newStore(t).Ensure(context.Background(), "  "); !errors.Is(err, ErrInvalid) {
		t.Errorf("Ensure(blank) = %v, want ErrInvalid", err)
	}
}

func TestByAddress(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p, err := s.Ensure(ctx, "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ByAddress(ctx, p.Address)
	if err != nil || got.Email != "ann@example.com" {
		t.Fatalf("ByAddress = %+v, %v", got, err)
	}
	// ADR-00032 obligation 3: an unknown or guessed address finds nothing, and
	// neither the email nor a malformed address finds the profile.
	for _, guess := range []string{
		"", "ann@example.com", "ann", strings.Repeat("A", 22), strings.Repeat("A", 21) + "!", p.Address + "A", p.Address[:21],
	} {
		if _, err := s.ByAddress(ctx, guess); !errors.Is(err, ErrNotFound) {
			t.Errorf("ByAddress(%q) = %v, want ErrNotFound", guess, err)
		}
	}
}

func TestRenameSetsAndClearsTheDisplayName(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p, err := s.Ensure(ctx, "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "ann" {
		t.Errorf("default name = %q, want the email's local part", p.Name())
	}
	if err := s.Rename(ctx, "ann@example.com", "  Ann the Baker "); err != nil {
		t.Fatal(err)
	}
	got, err := s.ByAddress(ctx, p.Address)
	if err != nil || got.Name() != "Ann the Baker" {
		t.Fatalf("after Rename: %+v, %v", got, err)
	}
	if err := s.Rename(ctx, "ann@example.com", "  "); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ByAddress(ctx, p.Address)
	if got.DisplayName != "" || got.Name() != "ann" {
		t.Errorf("a blank name did not restore the default: %+v", got)
	}
}

func TestRenameRefusesBadNamesWithAReasonAndMissingProfiles(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.Ensure(ctx, "ann@example.com"); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]string{
		"too long":          strings.Repeat("a", MaxNameRunes+1),
		"control character": "Ann\x07",
		"newline":           "Ann\nBaker",
		"invalid utf-8":     "Ann\xff",
	} {
		err := s.Rename(ctx, "ann@example.com", input)
		reason, ok := InvalidReason(err)
		if !ok || reason != err.Error() || !strings.Contains(reason, "name") {
			t.Errorf("%s: InvalidReason(%v) = %q, %v", name, err, reason, ok)
		}
	}
	if err := s.Rename(ctx, "ann@example.com", strings.Repeat("é", MaxNameRunes)); err != nil {
		t.Errorf("a name of exactly %d characters was refused: %v", MaxNameRunes, err)
	}
	if err := s.Rename(ctx, "nobody@example.com", "Nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Rename of a missing profile = %v, want ErrNotFound", err)
	}
	if _, ok := InvalidReason(ErrNotFound); ok {
		t.Error("InvalidReason accepted a different error")
	}
}
