package layout

import (
	"strconv"
	"time"

	"github.com/kaecyra/gobbler/internal/household"
)

// InvitationsPath is where an invitee opens the invitation with a given id.
const InvitationsPath = "/invitations"

// timeLayout is how an invitation time is shown; UTC, so the page does not
// depend on the server's zone.
const timeLayout = "2006-01-02 15:04 MST"

// FormatTime renders an invitation time the way every invitation page shows it.
func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// PendingInvitation is one waiting invitation as the household and operator
// pages list it.
type PendingInvitation struct {
	ID      int64
	Email   string
	Expires string
	// Path is where the invitee opens the invitation.
	Path string
	// Mine reports whether the reader issued it, and so may revoke it.
	Mine bool
}

// NewPendingInvitations builds the list view of in for the reader self.
func NewPendingInvitations(self string, in []household.Invitation) []PendingInvitation {
	out := make([]PendingInvitation, 0, len(in))
	for _, i := range in {
		out = append(out, PendingInvitation{
			ID:      i.ID,
			Email:   i.Email,
			Expires: FormatTime(i.ExpiresAt),
			Path:    InvitationsPath + "/" + strconv.FormatInt(i.ID, 10),
			Mine:    i.IssuedBy == self,
		})
	}
	return out
}
