package layout

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/mail"
)

// InvitationsPath is where an invitee opens an invitation, by its code
// (ADR-00028).
const InvitationsPath = "/invitations"

// InvitationCodeParam is the route parameter that names an invitation.
const InvitationCodeParam = "code"

// InvitationCode returns the invitation code the request's route names. A
// value not shaped like a code is ErrNotFound, the answer an unknown code
// gets, so the shape check reveals nothing either (ADR-00028).
func InvitationCode(r *http.Request) (string, error) {
	code := chi.URLParam(r, InvitationCodeParam)
	if !apihouseholds.ValidCode(code) {
		return "", fmt.Errorf("invitation code %q: %w", code, ErrNotFound)
	}
	return code, nil
}

// timeLayout is how an invitation time is shown; UTC, so the page does not
// depend on the server's zone.
const timeLayout = "2006-01-02 15:04 MST"

// FormatTime renders an invitation time the way every invitation page shows it.
func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// PendingInvitation is one waiting invitation as the household and operator
// pages list it.
type PendingInvitation struct {
	// Code names the invitation in its link and its revoke form.
	Code    string
	Email   string
	Expires string
	// Path is where the invitee opens the invitation.
	Path string
	// Mine reports whether the reader issued it, and so may revoke it.
	Mine bool
	// EmailStatus says when the invitation's email went out or why it did not
	// (ADR-00055); EmailFailed marks a latest attempt that failed.
	EmailStatus string
	EmailFailed bool
}

// NewPendingInvitations builds the list view of in for the reader self.
func NewPendingInvitations(self string, in []apihouseholds.Invitation) []PendingInvitation {
	out := make([]PendingInvitation, 0, len(in))
	for _, i := range in {
		sent := ""
		if !i.EmailedAt.IsZero() {
			sent = FormatTime(i.EmailedAt)
		}
		status, failed := mail.StatusLine(sent, i.EmailError)
		out = append(out, PendingInvitation{
			Code:        i.Code,
			Email:       i.Email,
			Expires:     FormatTime(i.ExpiresAt),
			Path:        InvitationsPath + "/" + i.Code,
			Mine:        i.IssuedBy == self,
			EmailStatus: status,
			EmailFailed: failed,
		})
	}
	return out
}
