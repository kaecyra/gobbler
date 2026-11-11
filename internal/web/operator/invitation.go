package operator

import (
	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/mail"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

type invitationsView struct {
	Pending []pendingRow
}

func revokePath(code string) string {
	return InvitationsPath + "/" + code + "/revoke"
}

func resendPath(code string) string {
	return InvitationsPath + "/" + code + "/resend"
}

// pendingRow is a pending invitation with what became of its email
// (ADR-00055).
type pendingRow struct {
	layout.PendingInvitation
	// Status says when the email went out or why it did not; Failed marks a
	// latest attempt that failed.
	Status string
	Failed bool
}

func newPendingRows(self string, in []apihouseholds.Invitation) []pendingRow {
	rows := layout.NewPendingInvitations(self, in)
	out := make([]pendingRow, len(rows))
	for i, r := range rows {
		sent := ""
		if !in[i].EmailedAt.IsZero() {
			sent = layout.FormatTime(in[i].EmailedAt)
		}
		out[i] = pendingRow{PendingInvitation: r}
		out[i].Status, out[i].Failed = mail.StatusLine(sent, in[i].EmailError)
	}
	return out
}
