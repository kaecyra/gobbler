package operator

import (
	"github.com/kaecyra/gobbler/internal/web/layout"
)

type invitationsView struct {
	Pending []layout.PendingInvitation
}

func revokePath(code string) string {
	return InvitationsPath + "/" + code + "/revoke"
}

func resendPath(code string) string {
	return InvitationsPath + "/" + code + "/resend"
}
