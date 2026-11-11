package operator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/kaecyra/gobbler/internal/web/layout"
)

// Every operator page carries the operator rail, so the promotion pages are
// reachable from the invitations page. The rail is only ever rendered on
// operator pages, which package web refuses to everyone but an operator.
func TestInvitationsPageLinksToPromotions(t *testing.T) {
	var b bytes.Buffer
	if err := invitationsPage(invitationsView{}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if want := `href="` + PromotionPattern + `"`; !strings.Contains(b.String(), want) {
		t.Errorf("invitations page lacks %s:\n%s", want, b.String())
	}
}

// Each pending invitation shows what became of its email and offers Resend
// (ADR-00055), whatever the status.
func TestInvitationsPageShowsEmailStatusAndResend(t *testing.T) {
	rows := []pendingRow{
		{PendingInvitation: layout.PendingInvitation{Code: "Aaaaaaaa", Email: "a@example.com", Path: "/invitations/Aaaaaaaa", Mine: true}, Status: "Not emailed"},
		{PendingInvitation: layout.PendingInvitation{Code: "Bbbbbbbb", Email: "b@example.com", Path: "/invitations/Bbbbbbbb", Mine: true}, Status: "Email failed: refused", Failed: true},
	}
	var b bytes.Buffer
	if err := invitationsPage(invitationsView{Pending: rows}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Not emailed", "Email failed: refused", `action="/operator/invitations/Aaaaaaaa/resend"`, `action="/operator/invitations/Bbbbbbbb/resend"`} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("page lacks %q:\n%s", want, b.String())
		}
	}
}
