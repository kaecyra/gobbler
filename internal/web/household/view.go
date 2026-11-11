package household

import (
	"strconv"

	apihouseholds "github.com/kaecyra/gobbler/internal/api/households"
	"github.com/kaecyra/gobbler/internal/mail"
	"github.com/kaecyra/gobbler/internal/web/layout"
)

// pageView is everything the members page shows.
type pageView struct {
	Name string
	// Self is the verified email of the reader.
	Self    string
	Members []string
	Pending []pendingRow
	// Keys are the household's API keys (keys.go).
	Keys []layout.APIKeyRow
	// HouseStyle is the household's house style (ADR-00048).
	HouseStyle string
}

// houseStyleMax is the house style's bound for the field's maxlength. The
// browser counts UTF-16 code units, not bytes, so the store's check is the one
// that holds; this only stops the common case before it is sent.
// textareaText is s as a textarea's content. The HTML parser drops one line
// break straight after <textarea>, so a leading one is sent to be dropped and
// a style that starts with a blank line keeps it when it is saved again.
func textareaText(s string) string { return "\n" + s }

func houseStyleMax() string { return strconv.Itoa(apihouseholds.MaxHouseStyleBytes) }

// OnlyMember reports whether the reader is the household's last member, who
// cannot leave and alone may delete it.
func (v pageView) OnlyMember() bool { return len(v.Members) == 1 }

func revokePath(code string) string {
	return Pattern + "/invitations/" + code + "/revoke"
}

func resendPath(code string) string {
	return Pattern + "/invitations/" + code + "/resend"
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

// acceptView is the page an invitee opens.
type acceptView struct {
	Code      string
	Self      string
	Kind      apihouseholds.Kind
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
