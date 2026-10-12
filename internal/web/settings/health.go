package settings

import (
	"context"
	"time"

	"github.com/kaecyra/gobbler/internal/backup"
)

// Health detail keys and values. The health route is the only one served
// without authentication (ADR-00003), so the entries carry a status word and a
// timestamp and nothing else: never a path, bucket, endpoint or error text.
const (
	HealthBackupKey     = "backup"
	HealthBackupTimeKey = "backup_last_success"

	BackupNever  = "never"
	BackupOK     = "ok"
	BackupFailed = "failed"
)

// StatusReader is what the health detail needs from the backup job.
type StatusReader interface {
	Status() backup.Status
}

// HealthDetails returns the function web.Deps.HealthDetails expects. The status
// is "never" before the first success, "failed" while the last run failed and
// "ok" otherwise. The last success time is RFC 3339 in UTC and present only
// once there has been one.
func HealthDetails(s StatusReader) func(ctx context.Context) map[string]string {
	return func(context.Context) map[string]string {
		st := s.Status()
		d := map[string]string{HealthBackupKey: BackupOK}
		switch {
		case st.LastError != "":
			d[HealthBackupKey] = BackupFailed
		case st.LastSuccess.IsZero():
			d[HealthBackupKey] = BackupNever
		}
		if !st.LastSuccess.IsZero() {
			d[HealthBackupTimeKey] = st.LastSuccess.UTC().Format(time.RFC3339)
		}
		return d
	}
}
