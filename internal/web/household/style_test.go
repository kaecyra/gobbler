package household

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The house style field on the household page (ADR-00048).

func TestAMemberSavesTheHouseStyleOnTheHouseholdPage(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com", "abe@example.com")
	s.household(t, "Beta", "bo@example.com")

	// The second member, not only the founder, may save it.
	const style = "Weights in grams.\r\nOne step per action & no \"to taste\"."
	rec := s.do(http.MethodPost, "/household/style", "abe@example.com", url.Values{"style": {style}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != Pattern {
		t.Fatalf("save = %d to %q, want 303 to %s:\n%s", rec.Code, rec.Header().Get("Location"), Pattern, rec.Body)
	}

	stored := strings.ReplaceAll(style, "\r\n", "\n")
	h, err := s.hs.ForEmail(context.Background(), "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.hs.HouseStyle(context.Background(), h.ID); err != nil || got != stored {
		t.Fatalf("stored style = %q, %v, want %q", got, err, stored)
	}

	page := s.do(http.MethodGet, "/household", "ann@example.com", nil)
	if page.Code != http.StatusOK || !strings.Contains(html.UnescapeString(page.Body.String()), stored) {
		t.Fatalf("page does not show the saved style (%d):\n%s", page.Code, page.Body)
	}
	other := s.do(http.MethodGet, "/household", "bo@example.com", nil)
	if strings.Contains(html.UnescapeString(other.Body.String()), "Weights in grams.") {
		t.Fatal("another household's page shows the style")
	}
}

func TestAHouseStyleOver16KiBIsRefusedOnTheHouseholdPage(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com")
	if rec := s.do(http.MethodPost, "/household/style", "ann@example.com", url.Values{"style": {"Keep this."}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("first save = %d", rec.Code)
	}
	rec := s.do(http.MethodPost, "/household/style", "ann@example.com", url.Values{"style": {strings.Repeat("a", 16385)}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "at most 16 KiB") {
		t.Fatalf("over-limit save = %d, want 400 with the reason:\n%s", rec.Code, rec.Body)
	}
	h, err := s.hs.ForEmail(context.Background(), "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.hs.HouseStyle(context.Background(), h.ID); got != "Keep this." {
		t.Fatalf("style after a refused save = %q", got)
	}
}

func TestANonMemberCannotSaveAHouseStyle(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com")
	rec := s.do(http.MethodPost, "/household/style", "stranger@example.com", url.Values{"style": {"Mine now."}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member save = %d, want 403", rec.Code)
	}
	h, err := s.hs.ForEmail(context.Background(), "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.hs.HouseStyle(context.Background(), h.ID); got != "" {
		t.Fatalf("style after a non-member's save = %q", got)
	}
}
