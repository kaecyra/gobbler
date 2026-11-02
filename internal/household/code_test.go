package household

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
)

// Generated codes are eight characters from the base62 alphabet (ADR-00028).
func TestNewCodeIsEightBase62Characters(t *testing.T) {
	seen := map[byte]bool{}
	for range 2000 {
		code, err := newCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != CodeLength || !ValidCode(code) {
			t.Fatalf("code %q is not %d base62 characters", code, CodeLength)
		}
		for i := 0; i < len(code); i++ {
			if !strings.ContainsRune(codeAlphabet, rune(code[i])) {
				t.Fatalf("code %q has %q, outside the alphabet", code, code[i])
			}
			seen[code[i]] = true
		}
	}
	// 16000 uniform draws miss a given character with odds near e^-258.
	if len(seen) != len(codeAlphabet) {
		t.Errorf("2000 codes used %d of the %d characters", len(seen), len(codeAlphabet))
	}
}

// A byte at or above 248 would make the first eight characters likelier than
// the rest, so it is thrown away and another byte drawn.
func TestNewCodeDiscardsBytesThatWouldBiasIt(t *testing.T) {
	// The source is read eight bytes at a time; nothing after 35 is used.
	src := []byte{248, 255, 0, 61, 62, 247, 9, 10, 36, 250, 35, 100, 1, 2, 3, 4}
	got, err := newCode(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	// 0→'0', 61→'z', 62→'0', 247→'z' (247%62=61), 9→'9', 10→'A', 36→'a', 35→'Z'.
	if want := "0z0z9AaZ"; got != want {
		t.Errorf("newCode = %q, want %q", got, want)
	}
}

func TestNewCodeFailsWhenTheSourceDoes(t *testing.T) {
	if _, err := newCode(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Error("newCode from a source that ran dry succeeded")
	}
}

func TestValidCode(t *testing.T) {
	for code, want := range map[string]bool{
		"aZ09bY18":  true,
		"00000000":  true,
		"aZ09bY1":   false,
		"aZ09bY18x": false,
		"aZ09-Y18":  false,
		"aZ09bY1é":  false,
		"":          false,
		"12":        false,
	} {
		if got := ValidCode(code); got != want {
			t.Errorf("ValidCode(%q) = %v, want %v", code, got, want)
		}
	}
}

// A drawn code that is already taken is drawn again, not reported.
func TestInviteRedrawsACodeAlreadyInUse(t *testing.T) {
	f := newFixture(t)
	h := f.create("Alpha", "ann@example.com")
	first := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	second := []byte{10, 11, 12, 13, 14, 15, 16, 17}
	f.s.rand = bytes.NewReader(append(append(append([]byte{}, first...), first...), second...))

	a := f.invite(Invite{Kind: KindJoin, Email: "cy@example.com", HouseholdID: h.ID, IssuedBy: "ann@example.com"})
	b := f.invite(Invite{Kind: KindHousehold, Email: "dee@example.com", IssuedBy: "op@example.com"})
	if a != "01234567" || b != "ABCDEFGH" {
		t.Fatalf("codes = %q, %q; want 01234567 and the redrawn ABCDEFGH", a, b)
	}
	if _, err := f.s.InvitationFor(context.Background(), b, "dee@example.com"); err != nil {
		t.Errorf("the redrawn invitation cannot be read by its code: %v", err)
	}
}

// A source that keeps producing taken codes is broken; Invite says so rather
// than spinning.
func TestInviteGivesUpWhenEveryDrawIsTaken(t *testing.T) {
	f := newFixture(t)
	same := bytes.Repeat([]byte{0, 1, 2, 3, 4, 5, 6, 7}, codeDraws+1)
	f.s.rand = bytes.NewReader(same)
	f.invite(Invite{Kind: KindHousehold, Email: "cy@example.com", IssuedBy: "op@example.com"})
	_, err := f.s.Invite(context.Background(), Invite{Kind: KindHousehold, Email: "dee@example.com", IssuedBy: "op@example.com"})
	if err == nil || errors.Is(err, errCodeTaken) {
		t.Fatalf("Invite after %d taken codes = %v, want a plain failure", codeDraws, err)
	}
}

// Codes are compared exactly: the same letters in another case are another
// code, and that one does not exist.
func TestInvitationCodesAreCaseSensitive(t *testing.T) {
	f := newFixture(t)
	f.s.rand = bytes.NewReader([]byte{10, 11, 12, 13, 14, 15, 16, 17})
	code := f.invite(Invite{Kind: KindHousehold, Email: "cy@example.com", IssuedBy: "op@example.com"})
	if _, err := f.s.InvitationFor(context.Background(), strings.ToLower(code), "cy@example.com"); !errors.Is(err, ErrNotFound) {
		t.Errorf("InvitationFor(%q) = %v, want ErrNotFound", strings.ToLower(code), err)
	}
}
