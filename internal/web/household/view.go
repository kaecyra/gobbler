package household

import (
	"strconv"

	domain "github.com/kaecyra/gobbler/internal/household"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// pageView is everything the members page shows.
type pageView struct {
	Name string
	// Self is the verified email of the reader.
	Self    string
	Members []string
	Pending []layout.PendingInvitation
}

// OnlyMember reports whether the reader is the household's last member, who
// cannot leave and alone may delete it.
func (v pageView) OnlyMember() bool { return len(v.Members) == 1 }

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
