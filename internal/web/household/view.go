package household

import (
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

func revokePath(code string) string {
	return Pattern + "/invitations/" + code + "/revoke"
}

// acceptView is the page an invitee opens.
type acceptView struct {
	Code      string
	Self      string
	Kind      domain.Kind
	Household string
	IssuedBy  string
	Expires   string
	// Member is set when the reader already belongs to a household, who must
	// leave it before accepting.
	Member bool
}

func acceptPath(code string) string {
	return InvitationsPattern + "/" + code + "/accept"
}
