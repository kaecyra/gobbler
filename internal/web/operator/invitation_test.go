package operator

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
