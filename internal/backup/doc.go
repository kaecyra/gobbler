// Package backup takes nightly SQLite snapshots with VACUUM INTO, uploads them
// to S3-compatible storage and applies local and remote retention (ADR-00010).
package backup
