# Operations

How to deploy gobbler, what every setting does, how backups work, and how to
restore from one. The reasoning is in the ADRs linked from each section.

Commands below that need Docker were **not run** when this was written (the
authoring environment has no Docker daemon); they are checked against
`compose.yaml`, the `Dockerfile` and the code, and marked "unverified" where
that matters. Run through them once on the real host and correct this file.

## Deploy

One Ubuntu VM, Docker Compose, a Cloudflare tunnel. The app publishes no host
port; the only way in is the tunnel, behind Cloudflare Access
([ADR-00003](./adr/00003-cloudflare-access-identity.md),
[ADR-00002](./adr/00002-single-binary-go-stack.md)).

### Prerequisites

- Docker Engine with the Compose plugin (`docker compose`), and `git`.
- A Cloudflare account with a domain on it and Zero Trust enabled.
- Outbound HTTPS from the VM: `cloudflared` to Cloudflare, `gobbler` to recipe
  sites, the LLM provider and the S3 endpoint.

### Cloudflare side (unverified: dashboard labels change)

1. In Zero Trust, create a tunnel and copy its token. That token is
   `CLOUDFLARE_TUNNEL_TOKEN`.
2. Give the tunnel a public hostname whose service is `http://gobbler:8080`
   (the Compose service name and port).
3. Create a self-hosted Access application for that hostname with a policy that
   allows only you.
4. Note two values for `.env`:
   - the **application AUD tag**, shown on the Access application, is
     `GOBBLER_ACCESS_AUD`;
   - your **team domain** (`<team>.cloudflareaccess.com`) is
     `GOBBLER_ACCESS_TEAM_DOMAIN`, written as a bare host without `https://`.
5. Set the Access application's cookie SameSite to **Lax** (unverified:
   Zero Trust > Access controls > Applications > Configure > Advanced settings
   > Cookie settings > SameSite). Cloudflare's default is None, which sends the
   Access cookie on cross-site requests; Lax makes Access refuse a cross-site
   POST at the edge. Do not use Strict: Cloudflare documents that it can cause
   `ERR_TOO_MANY_REDIRECTS`. See
   [ADR-00016](./adr/00016-cross-origin-protection.md).

gobbler verifies the Access JWT on every request except `/healthz`; without
both values it refuses to start.

### Bring it up

```sh
git clone https://github.com/kaecyra/gobbler.git && cd gobbler
cp .env.example .env     # fill in the tunnel token, team domain and AUD at least
docker compose up -d --build
```

`.env` is the only file you create. Compose builds the image, starts `gobbler`,
and starts `cloudflared` once `gobbler`'s health check passes. Migrations apply
at start-up before the server listens.

Verify:

```sh
docker compose ps                                   # gobbler: healthy
docker compose logs gobbler | grep listening        # JSON log line with the address
docker compose exec gobbler wget -q -O - http://127.0.0.1:8080/healthz
```

`/healthz` returns `{"status":"ok"}` and is the only route served without
authentication. Then open the public hostname: Access should prompt you to log
in, and the app should load.

### Upgrade

```sh
git pull
docker compose up -d --build
```

Pending migrations apply at start-up. If one fails, `gobbler` exits non-zero
before listening (ADR-00002 obligation 4); with `restart: unless-stopped`
Compose keeps retrying, so read `docker compose logs gobbler`, and restore the
database (below) if the fix is not obvious. `gobbler migrate` applies
migrations and exits, without serving.

## Configuration

All settings are environment variables, read once at start-up by
`internal/config`. Compose passes each one from `.env` by name. An empty or
blank value counts as unset and takes the default. An invalid value makes
`gobbler` exit at start-up with every offending variable named.

Compose forces `GOBBLER_LISTEN_ADDR=0.0.0.0:8080`, `GOBBLER_DATA_DIR=/data` and
`GOBBLER_DEV_AUTH_BYPASS=false`, whatever `.env` says. It does not set `TZ`, so
the container runs in UTC.

| Variable | Default | Secret | Notes |
|---|---|---|---|
| `CLOUDFLARE_TUNNEL_TOKEN` | none, required by Compose | yes | Read by `cloudflared` only, never by `gobbler`. |
| `GOBBLER_ACCESS_TEAM_DOMAIN` | none | no | Bare host, e.g. `team.cloudflareaccess.com`: no `https://`, path or spaces. Required unless the dev bypass is on. `config.Load` only checks it is set; `auth.New` rejects a malformed value, so a bad value fails at `serve` start-up. |
| `GOBBLER_ACCESS_AUD` | none | no | Application AUD tag. Required unless the dev bypass is on. |
| `GOBBLER_LISTEN_ADDR` | `127.0.0.1:8080` | no | |
| `GOBBLER_DATA_DIR` | `data` | no | Holds `gobbler.db` and `backups/`. |
| `GOBBLER_LOG_LEVEL` | `info` | no | `debug`, `info`, `warn` or `error`. |
| `GOBBLER_DEV_AUTH_BYPASS` | `false` | no | Skips Access verification; refuses to start unless the listen address is loopback. Development only. |
| `GOBBLER_INGEST_CONFIDENCE_THRESHOLD` | `0.7` | no | 0 to 1. Drafts below it fall back to the LLM ([ADR-00006](./adr/00006-rule-first-ingestion.md)). |
| `GOBBLER_INGEST_FETCH_TIMEOUT` | `15s` | no | Go duration, positive. |
| `GOBBLER_INGEST_USER_AGENT` | `gobbler (self-hosted recipe manager)` | no | |
| `GOBBLER_LLM_PROVIDER` | empty (LLM off) | no | `claude` or `gemini` ([ADR-00007](./adr/00007-llm-provider-interface.md)). |
| `GOBBLER_LLM_TEXT_MODEL` | none | no | Required with a provider; set in configuration only. |
| `GOBBLER_LLM_VISION_MODEL` | none | no | Required with a provider. |
| `GOBBLER_LLM_TIMEOUT` | `60s` | no | Go duration, positive. |
| `GOBBLER_ANTHROPIC_API_KEY` | none | yes | Required when the provider is `claude`. |
| `GOBBLER_GEMINI_API_KEY` | none | yes | Required when the provider is `gemini`. |
| `GOBBLER_TODOIST_TOKEN` | none | yes | Required when `GOBBLER_TODOIST_PROJECT` is set. |
| `GOBBLER_TODOIST_PROJECT` | none | no | |
| `GOBBLER_BACKUP_SCHEDULE` | `03:00` | no | Daily run time, 24-hour `HH:MM`, in the container's time zone. |
| `GOBBLER_BACKUP_LOCAL_RETENTION` | `7` | no | Snapshots kept locally; minimum 1. |
| `GOBBLER_BACKUP_REMOTE_RETENTION` | `30` | no | Snapshots kept in S3; minimum 1. |
| `GOBBLER_S3_ENDPOINT` | none | no | Host, optionally with `http://` or `https://`; bare host means TLS. |
| `GOBBLER_S3_REGION` | none | no | |
| `GOBBLER_S3_BUCKET` | none | no | |
| `GOBBLER_S3_PREFIX` | empty (bucket root) | no | Leading slash dropped, one trailing slash added. |
| `GOBBLER_S3_ACCESS_KEY_ID` | none | yes | |
| `GOBBLER_S3_SECRET_ACCESS_KEY` | none | yes | |

Cross-field rules enforced at start-up:

- If the LLM provider is empty, the model and API key variables must be empty
  too; settings that would be silently ignored are an error.
- If any S3 variable is set, endpoint, region, bucket, access key id and secret
  access key are all required. Only the prefix is optional.

Secrets live only in `.env`, which is never committed. Credentials are redacted
in logs.

## Backups

Design and reasoning: [ADR-00010](./adr/00010-backups.md).

- A scheduler inside `gobbler serve` runs once a day at
  `GOBBLER_BACKUP_SCHEDULE`. A run is: `VACUUM INTO` a snapshot, upload it when
  S3 is configured, then apply retention.
- Snapshots are plain SQLite files named `gobbler-YYYYMMDDTHHMMSSZ.db` (UTC),
  so names sort oldest to newest. Locally they are in `<data dir>/backups/`,
  which is `/data/backups/` in the container, on the `gobbler-data` volume.
- Remotely the object key is `<prefix>/<same file name>`, or just the file name
  with no prefix.
- Retention keeps the newest `GOBBLER_BACKUP_LOCAL_RETENTION` locally and
  `GOBBLER_BACKUP_REMOTE_RETENTION` remotely, and deletes only files matching
  the snapshot name pattern. Remote retention runs only after a successful
  upload.
- With no S3 settings, snapshots stay local only and the start-up log says so.
  Local snapshots sit on the same volume as the database, so they do not
  protect against losing the VM or the volume. Configure S3 for that.
- A failed run, including a failed upload, is logged as `backup failed` and
  does not stop the app or the schedule.
- Run the job now (same steps as the nightly run):

  ```sh
  docker compose exec gobbler gobbler backup   # unverified
  ```

- Status (last success time, last error) is stored in
  `<data dir>/backups/status.json`. As of this writing nothing in the web UI or
  `/healthz` shows it yet: `serve` does not set `web.Deps.HealthDetails`, and
  no settings page exists on this branch. Check logs and `status.json` instead.

## Restore

Recovery point: up to a day, the time since the last snapshot. Anything written
after it is lost.

### 1. Choose a snapshot

Names are UTC timestamps; the newest is normally right. Read
`backups/status.json` for the last successful run. If you are restoring because
of a bad change rather than a lost disk, pick the newest snapshot from before
the change.

Local snapshots (unverified):

```sh
docker compose run --rm --no-deps --entrypoint ls gobbler -l /data/backups
```

Remote snapshots: list the bucket under your prefix with any S3 tool. For
example with the AWS CLI, using the same endpoint, bucket and prefix as in
`.env` (unverified):

```sh
aws s3 ls --endpoint-url https://ENDPOINT s3://BUCKET/PREFIX/
```

### 2. Get the snapshot onto the host and check it

Local snapshot, copied out of the volume (works while the container is
stopped), or a remote one downloaded:

```sh
docker compose cp gobbler:/data/backups/gobbler-20261012T030000Z.db .      # unverified
aws s3 cp --endpoint-url https://ENDPOINT s3://BUCKET/PREFIX/gobbler-20261012T030000Z.db .   # unverified
```

The image has no `sqlite3` CLI, so check it on the host:

```sh
sqlite3 gobbler-20261012T030000Z.db 'PRAGMA integrity_check;'
```

It must print exactly `ok`. Do not continue otherwise; pick another snapshot.

### 3. Stop the app

```sh
docker compose stop gobbler
```

Stopping `gobbler` alone leaves `cloudflared` running, which is harmless: the
app is simply unreachable until step 5.

### 4. Replace the database

The database is `gobbler.db` in the data directory, in WAL mode, so it has
`gobbler.db-wal` and `gobbler.db-shm` beside it. **Move those away with the
old file.** A leftover WAL from the old database must never sit next to the
restored one. After an unclean stop the WAL holds committed data, so the old
copy is only complete with its WAL.

This runs as the image's `gobbler` user, so the file ends up with the right
owner. It keeps the old database as `gobbler.db.pre-restore`, with its WAL and
shm as `gobbler.db.pre-restore-wal` and `gobbler.db.pre-restore-shm`, in case
the restore was a mistake. Each move happens only if the file exists, so it
also works on a fresh volume with no database, the usual case after losing the
VM. An earlier `.pre-restore` set is overwritten (unverified):

```sh
docker compose run --rm --no-deps -v "$PWD:/restore:ro" --entrypoint sh gobbler -c '
  set -e
  cd /data
  for ext in "" -wal -shm; do
    if [ -e "gobbler.db$ext" ]; then mv "gobbler.db$ext" "gobbler.db.pre-restore$ext"; fi
  done
  cp /restore/gobbler-20261012T030000Z.db gobbler.db
'
```

Delete the `gobbler.db.pre-restore*` files from the volume once you are satisfied.

### 5. Start and verify

```sh
docker compose up -d
docker compose ps                                   # gobbler: healthy
docker compose logs gobbler                         # no migration error
docker compose exec gobbler wget -q -O - http://127.0.0.1:8080/healthz
```

Migrations apply at start-up, so a snapshot older than the current schema is
brought forward automatically. Then open the app and confirm a recent recipe is
there. The next scheduled run takes a fresh snapshot of the restored database.

## Not covered here

- The settings page and its backup status display, and the manual "back up now"
  button (ADR-00010), are not on this branch. The `gobbler backup` subcommand
  is the manual trigger today.
- The Todoist settings are read and validated, but this document does not
  describe the push feature.
