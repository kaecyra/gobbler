package operator

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The invitations page is the operator's landing page, and the promotion pages
// have no other way in. The link is only ever rendered on this page, which
// package web refuses to everyone but an operator.
func TestInvitationsPageLinksToPromotions(t *testing.T) {
	var b bytes.Buffer
	if err := invitationsPage(invitationsView{}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if want := `href="` + PromotionPattern + `"`; !strings.Contains(b.String(), want) {
		t.Errorf("invitations page lacks %s:\n%s", want, b.String())
	}
}
