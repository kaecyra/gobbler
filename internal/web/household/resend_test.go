package household

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The email status and Resend button on each pending invitation (ADR-00055).
func TestPendingInvitationShowsEmailStatusAndResends(t *testing.T) {
	s := newSite(t)
	s.household(t, "Alpha", "ann@example.com", "abe@example.com")
	s.household(t, "Beta", "bo@example.com")

	rec := s.do(http.MethodPost, "/household/invitations", "ann@example.com", url.Values{"email": {"cy@example.com"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("invite = %d", rec.Code)
	}
	page := s.do(http.MethodGet, "/household", "ann@example.com", nil).Body.String()
	if !strings.Contains(page, "Not emailed") || !strings.Contains(page, ">Resend<") {
		t.Fatalf("pending row lacks its status or Resend button:\n%s", page)
	}

	h, err := s.hs.ForEmail(context.Background(), "ann@example.com")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.hs.PendingForHousehold(context.Background(), h.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	target := "/household/invitations/" + pending[0].Code + "/resend"
	if !strings.Contains(page, `action="`+target+`"`) {
		t.Errorf("page has no form posting to %s", target)
	}

	// Another household's member cannot resend it.
	if rec := s.do(http.MethodPost, target, "bo@example.com", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("other household's resend = %d, want 404", rec.Code)
	}
	// Any member of this household can; with no mailer the status says so.
	rec = s.do(http.MethodPost, target, "abe@example.com", url.Values{})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != Pattern {
		t.Fatalf("resend = %d to %q, want 303 to %s", rec.Code, rec.Header().Get("Location"), Pattern)
	}
	page = s.do(http.MethodGet, "/household", "ann@example.com", nil).Body.String()
	if !strings.Contains(page, "Email failed: email is not set up on this deployment") {
		t.Errorf("page does not show why nothing went out:\n%s", page)
	}
}
