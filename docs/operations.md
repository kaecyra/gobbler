# Operations

How to deploy gobbler, what every setting does, how backups work, and how to
restore from one. The reasoning is in the ADRs linked from each section.

Commands below that need Docker were **not run** when this was written (the
authoring environment has no Docker daemon); they are checked against
`compose.yaml`, the `Dockerfile` and the code, and marked "unverified" where
that matters. Run through them once on the real host and correct this file.

## Deploy

One Ubuntu VM, Docker Compose, a Cloudflare tunnel. `cloudflared` runs on the
VM guest, configured outside this repository by the owner's infrastructure
code, not in Compose. The app publishes port 8080 on every interface of the VM so a monitoring stack
can reach `/healthz` ([ADR-00027](./adr/00027-publish-on-all-interfaces.md)).
Anything that can reach the VM's address can reach the app without passing
Cloudflare, so restrict port 8080 to the monitoring host in the VM firewall or
cloud security group. `ufw` rules do not filter Docker-published ports unless
written for Docker's `DOCKER-USER` chain. The app's own identity check, the
sign-in session, stops anything else that gets through
([ADR-00030](./adr/00030-google-sign-in.md),
[ADR-00002](./adr/00002-single-binary-go-stack.md),
[ADR-00017](./adr/00017-cloudflared-on-the-guest.md)).

### Prerequisites

- Docker Engine with the Compose plugin (`docker compose`), and `git`.
- A Cloudflare account with a domain on it and Zero Trust enabled.
- Outbound HTTPS from the VM: the guest's `cloudflared` to Cloudflare, `gobbler` to recipe
  sites, the sign-in issuer, the LLM provider and the S3 endpoint.

### Cloudflare side (unverified: dashboard labels change)

1. The guest's tunnel is managed by your infrastructure code, not by this
   repository.
2. That code must route the public hostname to `http://127.0.0.1:8080` (the
   port Compose publishes; the same binding serves loopback).

The tunnel is the only Cloudflare piece. There is no Cloudflare Access
application in front of the app: it signs people in itself (next section).

### Sign-in with Google (OpenID Connect)

gobbler signs people in itself, as an OpenID Connect client of one issuer
([ADR-00030](./adr/00030-google-sign-in.md)). The issuer is configuration: the
steps below set it up with Google, and another OpenID Connect issuer (a
self-hosted broker, for example) needs only different values for the same
four variables. Without the dev bypass they are required: gobbler refuses to
start without them, naming each one that is missing.

**Google Cloud Console** (unverified: console labels change; Google has been
moving these pages under "Google Auth Platform"):

1. Create or pick a project for gobbler.
2. Configure the **OAuth consent screen**: user type **External**, an app name
   and support email. Add the scopes **`openid`** and **`email`**, nothing
   else. These are not sensitive scopes, so no Google verification is needed,
   but while the app's publishing status is **Testing** only the listed test
   users can sign in: add them, or publish the app to **In production**.
3. Create an **OAuth client ID** of type **Web application**. Add one
   **authorised redirect URI**: `<GOBBLER_PUBLIC_BASE_URL>/auth/callback`, for
   example `https://gobbler.example.com/auth/callback`, exactly, with no
   trailing slash. No JavaScript origin is needed.
4. Note the **client ID** and **client secret** for `.env`.

**`.env`:**

```sh
GOBBLER_OIDC_ISSUER=https://accounts.google.com
GOBBLER_OIDC_CLIENT_ID=<client ID>
GOBBLER_OIDC_CLIENT_SECRET=<client secret>
GOBBLER_PUBLIC_BASE_URL=https://gobbler.example.com
# Optional; 720h (30 days) unless set.
# GOBBLER_SESSION_LIFETIME=720h
```

gobbler verifies the session on every request except the public paths
(`/healthz`, `/canada`, `/static/*`, `/login` and `/auth/callback`;
[ADR-00029](./adr/00029-public-paths.md)), where it is optional. A private
page opened without a session sends the browser to `/login`, which
goes to the issuer and comes back to `/auth/callback` and then to the page
that was asked for; any other request without a session gets `401`. Only an
email the issuer reports as verified is accepted.
"Sign out" in the nav menu ends the session at once, and so does removing a
member from their household. Sessions last `GOBBLER_SESSION_LIFETIME` from
sign-in, however recently they were used.

**Upgrading from a release that used Cloudflare Access** (0.84.x or earlier)
is a one-time step. Cut over to sign-in on 0.84.x first, following the
cut-over steps in that release's `docs/operations.md`, and confirm that
sign-in works. Later releases ignore `GOBBLER_ACCESS_TEAM_DOMAIN` and
`GOBBLER_ACCESS_AUD` (delete them from `.env`) and refuse to start without the
`GOBBLER_OIDC_*` variables and `GOBBLER_PUBLIC_BASE_URL`. Then delete the
Access application for the hostname in the Cloudflare dashboard. The tunnel
stays. The email is the identity, as it was under Access, so anyone who
reached gobbler through Access with a non-Google address (the one-time PIN)
must be invited again under their Google address.

### Admission and the first household

Admission is decided in the app. Any Google account with a verified email can
sign in; membership is the lock. A verified email that is a
member of a household reaches the app. One that is not reaches only the public
paths (`/healthz`, `/canada`, `/static/*`, `/login` and `/auth/callback`,
which serve no household data), the "not a member" page, invitation
acceptance and sign-out. Nobody joins or starts a
household without an invitation addressed to their email.

The **operator** is whoever `GOBBLER_OPERATOR_EMAILS` lists (comma-separated).
An operator email reaches the operator's pages, today `/settings`
(backup, storage and health), without being a member of any household; a member
who is not an operator is refused there. Operator status lives only in
configuration and cannot be granted from inside the app. Set the variable
before first use: with no operator and no household nobody can issue the first
invitation. The first household is created like any other, by the operator
inviting an email (their own, if they want to use it) to start one.

### Bring it up

```sh
git clone https://github.com/kaecyra/gobbler.git && cd gobbler
cp .env.example .env     # fill in the GOBBLER_OIDC_* settings at least
docker compose pull && docker compose up -d
```

`.env` is the only file you create. Set `GOBBLER_VERSION` in it to a version
published at `ghcr.io/kaecyra/gobbler`; Compose refuses to start without it.
Compose pulls that image, starts `gobbler`, and publishes it on
port 8080. CI publishes the image on every merge to `main`, tagged with
`VERSION` and the commit SHA ([ADR-00026](./adr/00026-publish-images-to-ghcr.md)).
The package is created private by the first publish; the owner sets it public
once in GitHub's package settings, after which the pull needs no login. Migrations apply at start-up before the
server listens.

Verify:

```sh
docker compose ps                                   # gobbler: healthy
docker compose logs gobbler | grep listening        # JSON log line with the address
docker compose exec gobbler wget -q -O - http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/healthz             # from the host: the published port
```

`/healthz` returns `{"status":"ok"}` and is one of the public paths served
without authentication (ADR-00029). Then open a private page on the public
hostname: it should send you to sign in with Google and bring you back.

### Upgrade

```sh
git pull                 # compose.yaml and .env.example can change between releases
# set GOBBLER_VERSION in .env to the new version, then:
docker compose pull
docker compose up -d
```

Rolling back is the same with the previous version in `GOBBLER_VERSION`. A
rollback does not undo migrations; see Restore if the newer version migrated
the database.

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
| `GOBBLER_OIDC_ISSUER` | none | no | OpenID Connect issuer URL, `https://accounts.google.com` for Google ([ADR-00030](./adr/00030-google-sign-in.md)). Kept exactly as written, because ID tokens are matched against it character for character, and its discovery document must name it the same way. `https://` is required unless the dev bypass is on. Required, with the other three sign-in variables, unless the dev bypass is on. gobbler contacts the issuer at the first sign-in, not at start-up. |
| `GOBBLER_OIDC_CLIENT_ID` | none | no | OAuth client ID issued by the provider. |
| `GOBBLER_OIDC_CLIENT_SECRET` | none | yes | OAuth client secret. |
| `GOBBLER_PUBLIC_BASE_URL` | none | no | Origin the site is reached at, e.g. `https://gobbler.example.com`, with no path. The sign-in redirect URI is it plus `/auth/callback`, and must be registered with the issuer exactly. `https://` is required unless the dev bypass is on. |
| `GOBBLER_SESSION_LIFETIME` | `720h` (30 days) | no | Go duration, positive. A session ends this long after sign-in, however recently it was used. |
| `GOBBLER_OPERATOR_EMAILS` | empty (no operator) | no | Comma-separated bare email addresses, case-insensitive. Each is an operator: it reaches `/settings` and the other operator pages without being a household member ([ADR-00018](./adr/00018-households-as-tenants.md)). A malformed entry is an error. |
| `GOBBLER_LISTEN_ADDR` | `127.0.0.1:8080` | no | |
| `GOBBLER_DATA_DIR` | `data` | no | Holds `gobbler.db` and `backups/`. |
| `GOBBLER_LOG_LEVEL` | `info` | no | `debug`, `info`, `warn` or `error`. |
| `GOBBLER_DEV_AUTH_BYPASS` | `false` | no | Skips identity verification; refuses to start unless the listen address is loopback. Development only. Every request acts as `dev@localhost` unless it sends `X-Gobbler-Dev-Identity`, and `dev@localhost` and `other@localhost` are seeded as members of two separate households. With sign-in configured as well (an `http://` issuer is allowed then), the sign-in routes work against that issuer but the bypass still decides every request's identity. None of this exists with the bypass off. |
| `GOBBLER_INGEST_CONFIDENCE_THRESHOLD` | `0.7` | no | 0 to 1. Drafts below it fall back to the LLM ([ADR-00006](./adr/00006-rule-first-ingestion.md)). |
| `GOBBLER_INGEST_FETCH_TIMEOUT` | `15s` | no | Go duration, positive. |
| `GOBBLER_INGEST_USER_AGENT` | `gobbler (self-hosted recipe manager)` | no | |
| `GOBBLER_LLM_PROVIDER` | empty (LLM off) | no | `claude` or `gemini` ([ADR-00007](./adr/00007-llm-provider-interface.md)). |
| `GOBBLER_LLM_TEXT_MODEL` | none | no | Required with a provider; set in configuration only. |
| `GOBBLER_LLM_VISION_MODEL` | none | no | Required with a provider. |
| `GOBBLER_LLM_TIMEOUT` | `60s` | no | Go duration, positive. |
| `GOBBLER_VIDEO_THUMBNAIL_HOST` | `https://i.ytimg.com` | no | Origin a recipe video's thumbnail is downloaded from when a recipe with that video is saved ([ADR-00021](./adr/00021-youtube-video-on-a-recipe.md)): `http://` or `https://` and a host, no path. Leave it unset in production; the e2e suite points it at a local fixture so no test reaches YouTube. |
| `GOBBLER_ANTHROPIC_API_KEY` | none | yes | Required when the provider is `claude`. |
| `GOBBLER_GEMINI_API_KEY` | none | yes | Required when the provider is `gemini`. |
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
- `GOBBLER_OIDC_ISSUER`, `GOBBLER_OIDC_CLIENT_ID`,
  `GOBBLER_OIDC_CLIENT_SECRET` and `GOBBLER_PUBLIC_BASE_URL` are all required
  unless the dev bypass is on. With the bypass on they are optional, but if
  any of them is set, all four are required.

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
  `<data dir>/backups/status.json`. The operator's `/settings` page shows it,
  and `/healthz` reports it under `details`.

## Restore

Recovery point: up to a day, the time since the last snapshot. Anything written
after it is lost.

Every `docker compose` command below reads `compose.yaml`, which refuses to run
without `GOBBLER_VERSION` in `.env`, even for steps that do not start the app.
Keep it set to the version you are running.

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

Stopping `gobbler` alone leaves the guest's `cloudflared` running, which is
harmless: the app is simply unreachable until step 5.

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
