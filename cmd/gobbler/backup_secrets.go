package main

import "github.com/kaecyra/gobbler/internal/config"

// backupSecrets are the S3 credential values, for the settings page to redact
// from the last backup error it shows.
func backupSecrets(cfg config.Config) []string {
	return []string{cfg.Backup.S3.AccessKeyID.Reveal(), cfg.Backup.S3.SecretAccessKey.Reveal()}
}
