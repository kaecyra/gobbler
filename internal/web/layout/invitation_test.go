package layout

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kaecyra/gobbler/internal/household"
)

func TestFormatTimeIsUTC(t *testing.T) {
	zone := time.FixedZone("far", 9*60*60)
	got := FormatTime(time.Date(2026, 3, 4, 8, 5, 0, 0, zone))
	if want := "2026-03-03 23:05 UTC"; got != want {
		t.Errorf("FormatTime = %q, want %q", got, want)
	}
}

func TestNewPendingInvitationsViewsEachInvitation(t *testing.T) {
	exp := time.Date(2026, 3, 4, 8, 5, 0, 0, time.UTC)
	got := NewPendingInvitations("me@example.com", []household.Invitation{
		{Code: "aZ09bY18", Email: "a@example.com", IssuedBy: "me@example.com", ExpiresAt: exp, EmailedAt: exp.Add(time.Hour)},
		{Code: "Zz000000", Email: "b@example.com", IssuedBy: "other@example.com", ExpiresAt: exp, EmailError: "refused"},
	})
	want := []PendingInvitation{
		{Code: "aZ09bY18", Email: "a@example.com", Expires: "2026-03-04 08:05 UTC", Path: "/invitations/aZ09bY18", Mine: true, EmailStatus: "Emailed 2026-03-04 09:05 UTC"},
		{Code: "Zz000000", Email: "b@example.com", Expires: "2026-03-04 08:05 UTC", Path: "/invitations/Zz000000", Mine: false, EmailStatus: "Email failed: refused", EmailFailed: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d views, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("view %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestNewPendingInvitationsEmptyIsNotNil(t *testing.T) {
	if got := NewPendingInvitations("me@example.com", nil); got == nil || len(got) != 0 {
		t.Errorf("got %#v, want empty non-nil slice", got)
	}
}

func TestFailShowsTheStoresReasonOnlyForRefusedInput(t *testing.T) {
	refused := fmt.Errorf("%w: not an email address", household.ErrInvalid)
	rec := httptest.NewRecorder()
	Fail(rec, httptest.NewRequest(http.MethodPost, "/x", nil), refused)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("refused input status = %d, want 400", rec.Code)
	}
	if reason, _ := household.InvalidReason(refused); !strings.Contains(rec.Body.String(), reason) {
		t.Errorf("body does not show reason %q", reason)
	}

	rec = httptest.NewRecorder()
	Fail(rec, httptest.NewRequest(http.MethodPost, "/x", nil), errors.New("driver: secret detail"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("unmapped error status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Error("body leaks the error chain")
	}
}
