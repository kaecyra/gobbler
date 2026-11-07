package operator

import (
	"strconv"

	"github.com/kaecyra/gobbler/internal/backup"
)

// timeLayout is how the last backup is shown; UTC, like the deployment page.
const timeLayout = "2006-01-02 15:04 MST"

// overviewView is what each card of the overview says is waiting.
type overviewView struct {
	Pending   string
	Suggested string
	Backup    string
	// BackupFailed marks the backup line as an alert.
	BackupFailed bool
}

func newOverviewView(pending, suggested int, st backup.Status) overviewView {
	v := overviewView{
		Pending:   count(pending, "invitation is waiting", "invitations are waiting", "No invitations are waiting."),
		Suggested: count(suggested, "ingredient is waiting for review", "ingredients are waiting for review", "No ingredient is waiting for review."),
	}
	switch {
	case st.LastError != "":
		v.Backup, v.BackupFailed = "The last backup failed.", true
	case st.LastSuccess.IsZero():
		v.Backup = "No backup has completed yet."
	default:
		v.Backup = "Last successful backup: " + st.LastSuccess.UTC().Format(timeLayout)
	}
	return v
}

// count says how many of something are waiting, in a sentence.
func count(n int, one, many, none string) string {
	switch n {
	case 0:
		return none
	case 1:
		return "1 " + one + "."
	default:
		return strconv.Itoa(n) + " " + many + "."
	}
}
