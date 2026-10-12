// Package settings is the settings page: the state of the nightly backup, a
// "back up now" action that runs the same job, and the entries the
// unauthenticated health check reports about it (ADR-00010). It depends on the
// backup job through the small Backup interface and imports no other web
// feature package (ADR-00012). It is mounted from internal/web.
package settings
