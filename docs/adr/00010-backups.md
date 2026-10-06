# ADR-00010: Nightly SQLite snapshot to S3-compatible storage

**Status:** Accepted

**Date:** 2026-10-06

## Context

All of gobbler's state is one SQLite file (ADR-00002). Personal notes and edits
cannot be recreated from the source sites, so losing the file loses real work.
The VM is a single machine with no other copy.

Options considered:

- **Leave backups to the operator.** Easy to forget. Rejected.
- **Copy the database file directly.** Not safe while the app is writing, even
  in WAL mode. Rejected.
- **Litestream continuous replication.** Strong recovery point, but another
  process and more configuration than a personal app needs. Rejected for now.
- **In-app nightly `VACUUM INTO` snapshot, uploaded to S3-compatible storage.**
  Chosen.

## Decision

A scheduler inside the app runs once a day at a configured time:

1. `VACUUM INTO` a timestamped file in a local backup directory. This is a
   consistent snapshot taken while the app runs.
2. Upload the file to a configured S3-compatible endpoint (endpoint, region,
   bucket, prefix and credentials from the environment), so AWS S3,
   Backblaze B2, Cloudflare R2 or MinIO all work.
3. Keep a configured number of local snapshots and a configured number of
   remote snapshots, deleting older ones.

Each run logs success or failure. The last successful backup time shows on a
settings page and in the health check's detail output. A manual "back up now"
action runs the same job.

If no S3 endpoint is configured, snapshots stay local and the settings page
says so.

## Consequences

The recovery point is up to a day. Restoring is a documented manual step: stop
the app, replace the database file, start it.

**Obligations:**

1. A snapshot taken during writes opens and passes `PRAGMA integrity_check`.
2. Retention deletes only snapshots beyond the configured counts, both locally
   and remotely.
3. A failed upload is logged and shown on the settings page; it does not stop
   the app.
4. The restore procedure is written in the operations documentation.
