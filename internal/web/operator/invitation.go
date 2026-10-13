package operator

import (
	"strconv"

	"github.com/kaecyra/gobbler/internal/web/layout"
)

type invitationsView struct {
	Pending []layout.PendingInvitation
}

func revokePath(id int64) string {
	return Pattern + "/invitations/" + strconv.FormatInt(id, 10) + "/revoke"
}
