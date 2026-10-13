package operator

import (
	"strconv"

	"github.com/kaecyra/gobbler/internal/household"
)

// timeLayout is how an expiry is shown; UTC, so the page does not depend on
// the server's zone.
const timeLayout = "2006-01-02 15:04 MST"

type invitationsView struct {
	Pending []pendingView
}

type pendingView struct {
	ID      int64
	Email   string
	Expires string
	// Path is where the invitee opens the invitation.
	Path string
}

func newInvitationsView(in []household.Invitation) invitationsView {
	v := invitationsView{Pending: make([]pendingView, 0, len(in))}
	for _, i := range in {
		v.Pending = append(v.Pending, pendingView{
			ID:      i.ID,
			Email:   i.Email,
			Expires: i.ExpiresAt.UTC().Format(timeLayout),
			Path:    "/invitations/" + strconv.FormatInt(i.ID, 10),
		})
	}
	return v
}

func revokePath(id int64) string {
	return Pattern + "/invitations/" + strconv.FormatInt(id, 10) + "/revoke"
}
