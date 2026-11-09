package households_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/api/households"
)

// The house style (ADR-00048): any member saves it, it is empty by default,
// and it is bounded at 16 KiB.

func TestTheHouseStyleIsEmptyUntilAMemberSavesIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	got, err := f.svc.HouseStyle(ctx, f.ann)
	if err != nil || got != "" {
		t.Fatalf("HouseStyle before a save = %q, %v, want empty", got, err)
	}
	const style = "Weights in grams.\nOne step per action."
	if err := f.svc.SetHouseStyle(ctx, f.ann, style); err != nil {
		t.Fatalf("SetHouseStyle = %v", err)
	}
	if got, err := f.svc.HouseStyle(ctx, f.ann); err != nil || got != style {
		t.Fatalf("HouseStyle = %q, %v, want %q", got, err, style)
	}
	// It is the Smiths' alone.
	if got, err := f.svc.HouseStyle(ctx, f.jo); err != nil || got != "" {
		t.Fatalf("Joneses' HouseStyle = %q, %v, want empty", got, err)
	}
}

func TestSavingTheHouseStyleChangesNothingElseAboutTheHousehold(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.svc.SetHouseStyle(ctx, f.ann, "Metric only."); err != nil {
		t.Fatal(err)
	}
	h, err := f.store.ForEmail(ctx, annEmail)
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != f.smiths.ID || h.Name != f.smiths.Name || !h.CreatedAt.Equal(f.smiths.CreatedAt) {
		t.Fatalf("household after a save = %+v, want %+v", h, f.smiths)
	}
	ms, err := f.svc.Members(ctx, f.ann)
	if err != nil || len(ms) != 1 || ms[0] != annEmail {
		t.Fatalf("members after a save = %v, %v", ms, err)
	}
}

func TestAHouseStyleOver16KiBIsRefusedWithAMessage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.svc.SetHouseStyle(ctx, f.ann, "Keep this."); err != nil {
		t.Fatal(err)
	}
	if households.MaxHouseStyleBytes != 16384 {
		t.Fatalf("MaxHouseStyleBytes = %d, want 16384", households.MaxHouseStyleBytes)
	}
	err := f.svc.SetHouseStyle(ctx, f.ann, strings.Repeat("a", households.MaxHouseStyleBytes+1))
	if !errors.Is(err, households.ErrInvalid) {
		t.Fatalf("SetHouseStyle(16 KiB + 1) = %v, want ErrInvalid", err)
	}
	reason, ok := households.InvalidReason(err)
	if !ok || !strings.Contains(reason, "16 KiB") {
		t.Fatalf("reason = %q, %v, want one naming the 16 KiB bound", reason, ok)
	}
	if got, _ := f.svc.HouseStyle(ctx, f.ann); got != "Keep this." {
		t.Fatalf("HouseStyle after a refused save = %q, want the old one", got)
	}
	// Exactly at the bound is accepted.
	if err := f.svc.SetHouseStyle(ctx, f.ann, strings.Repeat("a", households.MaxHouseStyleBytes)); err != nil {
		t.Fatalf("SetHouseStyle(16 KiB) = %v", err)
	}
}

func TestTheHouseStyleBoundCountsBytesAfterLineBreaksAreNormalised(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	// 8192 CRLF pairs are 16 KiB as sent and 8 KiB as stored.
	if err := f.svc.SetHouseStyle(ctx, f.ann, strings.Repeat("\r\n", 8192)); err != nil {
		t.Fatalf("SetHouseStyle(CRLF) = %v", err)
	}
	if got, _ := f.svc.HouseStyle(ctx, f.ann); got != strings.Repeat("\n", 8192) {
		t.Fatalf("stored %d bytes, want 8192 LF", len(got))
	}
	// A multi-byte character is counted in bytes: 5462 three-byte runes are
	// 16386 bytes.
	if err := f.svc.SetHouseStyle(ctx, f.ann, strings.Repeat("€", 5462)); !errors.Is(err, households.ErrInvalid) {
		t.Fatalf("SetHouseStyle(16386 bytes of €) = %v, want ErrInvalid", err)
	}
}

func TestAHouseholdKeyReadsTheHouseStyleButCannotSaveIt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.svc.SetHouseStyle(ctx, f.ann, "Metric only."); err != nil {
		t.Fatal(err)
	}
	key := households.Caller{Household: f.smiths, HouseholdKey: true}
	if got, err := f.svc.HouseStyle(ctx, key); err != nil || got != "Metric only." {
		t.Fatalf("HouseStyle as a household key = %q, %v", got, err)
	}
	if err := f.svc.SetHouseStyle(ctx, key, "Imperial."); !errors.Is(err, households.ErrNotMember) {
		t.Fatalf("SetHouseStyle as a household key = %v, want ErrNotMember", err)
	}
}

func TestReadingTheHouseStyleRefusesANonMember(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if err := f.svc.SetHouseStyle(ctx, f.ann, "Secret rules."); err != nil {
		t.Fatal(err)
	}
	cases := map[string]households.Caller{
		"a person of another household":        f.stranger,
		"a person who names another household": {Person: joEmail, Household: f.smiths},
		"no person":                            {Household: f.smiths},
		"no household":                         {Person: annEmail},
	}
	for who, c := range cases {
		if got, err := f.svc.HouseStyle(ctx, c); !errors.Is(err, households.ErrNotMember) || got != "" {
			t.Errorf("HouseStyle as %s = %q, %v, want ErrNotMember", who, got, err)
		}
	}
}
