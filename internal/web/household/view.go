package household

import (
	"strconv"

	domain "github.com/kaecyra/gobbler/internal/household"
)

// timeLayout is how an expiry is shown; UTC, so the page does not depend on
// the server's zone.
const timeLayout = "2006-01-02 15:04 MST"

// pageView is everything the members page shows.
type pageView struct {
	Name string
	// Self is the verified email of the reader.
	Self    string
	Members []string
	Pending []pendingView
}

// OnlyMember reports whether the reader is the household's last member, who
// cannot leave and alone may delete it.
func (v pageView) OnlyMember() bool { return len(v.Members) == 1 }

type pendingView struct {
	ID      int64
	Email   string
	Expires string
	// Path is where the invitee opens the invitation.
	Path string
	// Mine reports whether the reader issued it, and so may revoke it.
	Mine bool
}

func newPendingViews(self string, in []domain.Invitation) []pendingView {
	out := make([]pendingView, 0, len(in))
	for _, i := range in {
		out = append(out, pendingView{
			ID:      i.ID,
			Email:   i.Email,
			Expires: i.ExpiresAt.UTC().Format(timeLayout),
			Path:    InvitationsPattern + "/" + strconv.FormatInt(i.ID, 10),
			Mine:    i.IssuedBy == self,
		})
	}
	return out
}

func revokePath(id int64) string {
	return Pattern + "/invitations/" + strconv.FormatInt(id, 10) + "/revoke"
}

// acceptView is the page an invitee opens.
type acceptView struct {
	ID        int64
	Self      string
	Kind      domain.Kind
	Household string
	IssuedBy  string
	Expires   string
	// Member is set when the reader already belongs to a household, who must
	// leave it before accepting.
	Member bool
}

func acceptPath(id int64) string {
	return InvitationsPattern + "/" + strconv.FormatInt(id, 10) + "/accept"
}
