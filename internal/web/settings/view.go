package settings

import (
	"github.com/kaecyra/gobbler/internal/backup"
)

// timeLayout is how the last success is shown; UTC, so the page does not
// depend on the server's zone.
const timeLayout = "2006-01-02 15:04 MST"

// Outcome of a "back up now" request, shown beside the status.
const (
	outcomeNone    = ""
	outcomeOK      = "Backup finished."
	outcomeFailed  = "Backup failed."
	outcomeRunning = "A backup is already running."
)

// backupView is everything the backup section shows.
type backupView struct {
	// LastSuccess is empty when there has been no successful backup.
	LastSuccess string
	// LastError is the last failed run's error, empty after a success.
	LastError string
	// Remote reports whether snapshots are also uploaded to S3.
	Remote bool
	// Outcome is the result of the request that produced this view.
	Outcome string
}

func newBackupView(st backup.Status, outcome string) backupView {
	v := backupView{
		LastError: st.LastError,
		Remote:    st.Destination == backup.DestinationRemote,
		Outcome:   outcome,
	}
	if !st.LastSuccess.IsZero() {
		v.LastSuccess = st.LastSuccess.UTC().Format(timeLayout)
	}
	return v
}
