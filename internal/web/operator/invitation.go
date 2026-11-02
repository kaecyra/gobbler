package operator

import (
	"github.com/kaecyra/gobbler/internal/web/layout"
)

type invitationsView struct {
	Pending []layout.PendingInvitation
}

func revokePath(code string) string {
	return Pattern + "/invitations/" + code + "/revoke"
}
