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
- Outbound fetches of a URL someone else chose (a recipe page, a recipe picture,
  an OAuth client's metadata document) ignore `HTTP_PROXY` and `HTTPS_PROXY`, and
  never connect to a private, loopback or link-local address, even through a
  redirect or a name that resolves to one. A recipe cannot be imported by URL
  from a host on the local network. The one exception is a development server
  running with `GOBBLER_DEV_AUTH_BYPASS`, whose recipe page and picture fetches
  may connect to loopback; the OAuth metadata fetch never may
  ([ADR-00039](./adr/00039-recipe-pictures.md),
  [ADR-00040](./adr/00040-loopback-fetches-under-the-dev-bypass.md)).

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
(`/healthz`, `/canada`, `/static/*`, `/login`, `/auth/callback`, the OAuth
discovery documents under `/.well-known/`, `/oauth/register` and `/oauth/token`;
[ADR-00029](./adr/00029-public-paths.md)), where it is optional. A private
page opened without a session sends the browser to `/login`, which
goes to the issuer and comes back to `/auth/callback` and then to the page
that was asked for; any other request without a session gets `401`. Only an
email the issuer reports as verified is accepted.
"Sign out" in the account (avatar) menu ends the session at once, and so does removing a
member from their household. Sessions last `GOBBLER_SESSION_LIFETIME` from
sign-in, however recently they were used.

A session row records, beside its start, last use and expiry, the browser's
user agent and the client IP address, both taken when the session starts
([ADR-00032](./adr/00032-profiles.md)). Behind the Cloudflare tunnel the
address is the connection's remote address, or the `CF-Connecting-IP` header
when the connection comes from a trusted proxy (`GOBBLER_TRUSTED_PROXIES`, see
[Trusted proxies](#trusted-proxies)) and the header is an IP
address. The person sees them, with
the browser and system read from the user agent, in the Sessions section of
their own profile, where they can end one session or every session but the
current one; nobody else sees them. The address is for display only and never
admits or refuses anyone ([ADR-00027](./adr/00027-publish-on-all-interfaces.md)). Both are deleted with
the row when the session ends or expires. Sessions that began before the app recorded
these details show them empty.

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
| `GOBBLER_DATA_DIR` | `data` | no | Holds `gobbler.db`, `backups/` and, when no S3 media bucket is set, `media/`. |
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
| `GOBBLER_MCP_RATE` | `5` | no | Requests per second each credential may sustain at `/mcp`, 0.01 to 1000 ([ADR-00034](./adr/00034-mcp-server.md)). See [Connect an MCP client](#connect-an-mcp-client-with-a-key). |
| `GOBBLER_MCP_BURST` | `50` | no | Requests a credential may make at once before it is held to the rate; minimum 1. |
| `GOBBLER_TRUSTED_PROXIES` | empty | no | Comma-separated IP addresses or CIDR ranges whose `CF-Connecting-IP` header is believed ([ADR-00036](./adr/00036-rate-limits-on-public-oauth-endpoints.md)). Empty means the header is never read. See [Trusted proxies](#trusted-proxies). |
| `GOBBLER_OAUTH_REGISTER_RATE` | `0.1` | no | Client registrations per second each address (an IPv6 address by its /64) may sustain at `/oauth/register`, 0.001 to 1000. |
| `GOBBLER_OAUTH_REGISTER_BURST` | `10` | no | Registrations one address (an IPv6 address by its /64) may make at once before it is held to the rate; minimum 1. |
| `GOBBLER_OAUTH_TOKEN_RATE` | `1` | no | Requests per second each address (an IPv6 address by its /64) may sustain at `/oauth/token`, 0.001 to 1000. |
| `GOBBLER_OAUTH_TOKEN_BURST` | `20` | no | Requests one address (an IPv6 address by its /64) may make at once before it is held to the rate; minimum 1. |
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
| `GOBBLER_S3_MEDIA_BUCKET` | none | no | Bucket for uploaded media ([ADR-00033](./adr/00033-media-object-storage.md)), on the same endpoint and credentials as the backup bucket. Empty keeps media in `<data dir>/media/`. See [Media](#media). |

Cross-field rules enforced at start-up:

- If the LLM provider is empty, the model and API key variables must be empty
  too; settings that would be silently ignored are an error.
- If any S3 variable is set, endpoint, region, bucket, access key id and secret
  access key are all required. Only the prefix and the media bucket are
  optional.
- `GOBBLER_OIDC_ISSUER`, `GOBBLER_OIDC_CLIENT_ID`,
  `GOBBLER_OIDC_CLIENT_SECRET` and `GOBBLER_PUBLIC_BASE_URL` are all required
  unless the dev bypass is on. With the bypass on they are optional, but if
  any of them is set, all four are required.

Secrets live only in `.env`, which is never committed. Credentials are redacted
in logs.

## API keys

An API key is a credential for the machine-facing surface, so an agent or script
can act without a browser ([ADR-00031](adr/00031-api-keys.md)). There are two
kinds. The one route that accepts a key is the MCP endpoint
([ADR-00034](adr/00034-mcp-server.md)); the HTML pages ignore keys.

| Kind | Acts | Managed on |
|---|---|---|
| Personal | as one person, reaching their household | that person's profile page, under "API keys" (only they see it) |
| Household | for the household, not as a member | the household page, under "API keys" (any member may make or revoke one) |

- **Making a key.** Give it a name and an expiry: 30 days, 90 days (the
  default), 1 year, or no expiry. The secret, which starts `gob_`, is shown once,
  on the page that makes the key. Only its SHA-256 and a short prefix, which
  identifies the key in the list, are stored. A lost secret cannot be shown
  again: revoke the key and make another.
- **Revoking.** Revoking a key deletes it. A person revokes their own personal
  keys; any member revokes a household key.
- **Leaving.** When a member is removed or leaves, their personal keys and the
  household keys they made are revoked in the same change, because they have seen
  those secrets. Deleting a household revokes its household keys.
- **Logs.** No log line contains a key.

### Connect an MCP client with a key

gobbler serves the Model Context Protocol at `/mcp` over Streamable HTTP
([ADR-00034](adr/00034-mcp-server.md)). There is no stdio server. A client
sends the key as `Authorization: Bearer <key>`.

1. Make a key (see above). A personal key acts as that person; a household key
   acts for the household. Either reaches only that household's recipes,
   shopping lists and pantry. No key can manage keys, sessions, membership,
   invitations or operator pages.
2. Point the client at `https://<your host>/mcp` and give it the key as a
   header. A client that reads a JSON configuration of HTTP servers takes an entry
   like this one (an example shape; check your client's documentation for its
   exact field names):

   ```json
   {
     "mcpServers": {
       "gobbler": {
         "type": "http",
         "url": "https://gobbler.example.com/mcp",
         "headers": { "Authorization": "Bearer gob_..." }
       }
     }
   }
   ```

   The field names differ between clients; what gobbler needs is the URL and
   that one header. Keep the key out of anything committed.

What a client can do:

| Tools | Effect |
|---|---|
| `search_recipes`, `list_recipes`, `get_recipe` | Read recipes as structured data. |
| `import_recipe` (a URL or pasted text), `create_recipe_draft` | Produce a **draft** through the rule-first pipeline. It waits on the review screen (`/ingest/<token>`, returned as `review_path`) for a person to confirm; nothing is saved until then. A draft is stored durably (ADR-00035), shared with the web ingest form, and opens for the members of its household only. It survives a restart and expires after 365 days; a household holds at most 20 pending drafts of new recipes, and the 21st is refused with a message saying drafts are waiting for review (nothing is dropped to make room). A rejected, superseded or expired proposal is deleted 60 days after it reached that state, with its unconfirmed source. |
| `propose_recipe_edit`, `get_recipe_proposal` | Propose a change to an existing recipe: the whole recipe as it should be, the version read with `get_recipe`, and a reason. The recipe is **unchanged** until a person opens `review_path` (the recipe's editor, with the proposal filled in) and saves it. The proposal shows on the recipe page with its reason and the credential that made it, and the accepted edit records both in the recipe's history. A recipe has one pending proposal; a newer one replaces it. If the recipe is saved first, the proposal is stale: `get_recipe_proposal` says so with the recipe's current version, and the agent proposes again. A line whose ingredient the catalog does not have is refused rather than added. |
| `list_shopping_lists`, `get_shopping_list`, `get_pantry` | Read shopping lists and the pantry. |
| `create_shopping_list`, `add_recipe_to_shopping_list`, `add_manual_shopping_item`, `check_shopping_item` | Change shopping lists. These **apply at once**. |

Every write is recorded in the `api_key_writes` table (for a key) or the
`oauth_grant_writes` table (for an OAuth client, see below). Each row names the
credential by its id and by a snapshot of it (a key's prefix, owner and creation
time; a grant's client, owner and creation time), because SQLite reuses the id of
a revoked credential for a later one. The rows outlive the credential. `propose_recipe_edit` records a proposal, never a save; `get_recipe_proposal` reports whether it is still pending or has gone stale.
A tool that lists things returns one page of at most 50
and a `next_cursor` for the next.

A key that is missing, unknown, expired or revoked gets `401` with
`WWW-Authenticate: Bearer resource_metadata="<GOBBLER_PUBLIC_BASE_URL>/.well-known/oauth-protected-resource/mcp"`,
which is where an OAuth client learns how to sign in. A key whose owner belongs
to no household gets `403`. A session cookie never opens `/mcp`.

Each credential has its own leaky bucket: `GOBBLER_MCP_RATE` requests per
second drain it, and `GOBBLER_MCP_BURST` requests fit at once. A request past
that gets `429` with `Retry-After` in seconds. The defaults (5 a second, a burst
of 50) suit one home server and a few agents; raise them for a busy automation.

## OAuth for MCP clients

Some MCP clients, including hosted connector screens, can only sign in through
OAuth. gobbler is an OAuth 2.1 authorization server for them, and needs no
setting of its own: the issuer and the audience of every token come from
`GOBBLER_PUBLIC_BASE_URL` ([ADR-00034](adr/00034-mcp-server.md)). The audience is
`<GOBBLER_PUBLIC_BASE_URL>/mcp`, so changing the base URL makes every existing
token fail its audience check and every client has to be allowed again.

- **Connecting a client.** Give it the MCP address, `<GOBBLER_PUBLIC_BASE_URL>/mcp`.
  The client reads `/.well-known/oauth-protected-resource/mcp` and
  `/.well-known/oauth-authorization-server`, identifies itself, and opens
  `/oauth/authorize` in the browser. If you are not signed in, gobbler sends you
  to Google first and brings you back. The consent page names the app and the
  host your browser returns to. Allow it only if you started the connection.
  Only a member of a household can allow an app.
- **How a client identifies itself.** By Dynamic Client Registration at
  `/oauth/register`, or by using the URL of a Client ID Metadata Document as its
  `client_id`. Clients are public and use PKCE (`S256`) and the `resource`
  parameter, so gobbler issues and stores no client secret. gobbler deletes
  clients that never received a grant after a day, and keeps at most 10,000 of
  those; a registration past that is refused until the pruning makes room.
  Clients with a grant are never pruned and do not count towards the 10,000.
- **Fetching a metadata document.** The URL must be https. gobbler never connects
  to a private, loopback or link-local address, follows no redirect, and gives up
  after 5 seconds or 16 KiB. A client that lives on your own network is refused
  this way and has to register instead.
- **Using a token.** `/mcp` accepts an access token as `Authorization: Bearer
  goa_...` beside API keys. The token acts as the person who allowed the app, in
  their household, with the reach of a personal key: the same tools, the same
  paging, the same rate limit (a bucket for each grant) and the same rule that
  recipe changes wait on the review screen as drafts. Every write is recorded in
  `oauth_grant_writes` against the grant. A missing, unknown, expired or revoked
  token, or one issued for another address, gets `401` with the same
  `WWW-Authenticate` header as a key. A token never opens an HTML page.
- **Lifetimes.** A code lasts 5 minutes and works once. An access token lasts 1
  hour. A refresh token lasts 30 days and is replaced each time it is used. If a
  replaced refresh token, or a used code, is presented again, gobbler revokes the
  whole grant. Only SHA-256 hashes of codes and tokens are stored.
- **Where grants are revoked.** On the person's profile, under "Connected apps",
  where only they see the list. Revoking ends the app's access at once. When a
  member is removed or leaves, their grants are revoked in the same change that
  revokes their keys.
- **Logs.** No log line contains a code, a token or a verifier.
- **Limits.** The registration and token endpoints are public, so each client
  address has its own leaky bucket for each, except that an IPv6 address shares
  one with the rest of its /64, the block one subscriber holds
  ([ADR-00036](adr/00036-rate-limits-on-public-oauth-endpoints.md)):
  `GOBBLER_OAUTH_REGISTER_RATE` and `GOBBLER_OAUTH_REGISTER_BURST` for
  `/oauth/register` (by default 1 every 10 seconds, 10 at once), and
  `GOBBLER_OAUTH_TOKEN_RATE` and `GOBBLER_OAUTH_TOKEN_BURST` for `/oauth/token`
  (1 a second, 20 at once). A request past the burst gets `429` with
  `Retry-After` in seconds. They suit one home server; raise them if several
  people connect clients from behind one address. Browsers cannot call these
  endpoints from another origin.

### Trusted proxies

Behind the Cloudflare tunnel the caller's address arrives in the
`CF-Connecting-IP` header, but gobbler listens on every interface
([ADR-00027](adr/00027-publish-on-all-interfaces.md)), so anything on the private
network could send that header with any value. gobbler therefore uses the
connection's own address as the client address, and reads the header only when
the connection comes from an address in `GOBBLER_TRUSTED_PROXIES`. With it empty
every caller behind the tunnel looks like the cloudflared container: the
per-address limits above then share one bucket for all of them, and the
Sessions list shows the proxy's address.

Set it to the address cloudflared connects from, as the gobbler container sees
it. cloudflared runs on the VM guest and reaches `127.0.0.1:8080`, which Docker
forwards into the container, so the connection arrives from the gateway of the
Compose network, not from a container. Read the gateway with
`docker network inspect <project>_default` and list that one address:

```
GOBBLER_TRUSTED_PROXIES=172.18.0.1
```

Check it after setting: sign in through the tunnel and confirm the Sessions
list shows your own address, not the gateway's. List only the proxy: any
address in the list can set the client address of its requests to any value.

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

## Media

Design and reasoning: [ADR-00033](./adr/00033-media-object-storage.md).

What people upload (today, avatars) is not in SQLite. gobbler processes it to
a stripped WebP and keeps it as objects, read and written through one storage
interface with two implementations:

- **S3 media bucket.** Set `GOBBLER_S3_MEDIA_BUCKET` and the other S3 settings
  and media goes to that bucket. It is a separate bucket from
  `GOBBLER_S3_BUCKET` (the backups), on the same endpoint with the same
  credentials, so create both. Objects are under `people/<address>/`: the
  processed avatar as `people/<address>/avatar/<version>.webp` and, until
  processing succeeds, the upload and its crop preview as
  `people/<address>/uploads/<token>.orig` and `.preview.jpg`.
- **Local directory.** With no S3 settings, or S3 set for backups only (no media
  bucket), media is kept under `<data dir>/media/`, which is `/data/media/` in
  the container, on the `gobbler-data` volume. Development, the e2e suite and a
  deployment with no S3 work this way. Moving to S3 later does not copy what is
  already there; those avatars are as good as lost (see below).

The storage server is never exposed. gobbler reads the bytes itself and serves
them after the same admission as the page that shows them, so no URL points at
the bucket; keep the endpoint reachable from the gobbler host only.

**Media is not in the database snapshot.** The nightly `VACUUM INTO` copies
`gobbler.db` alone (see [Backups](#backups)), so a restore brings back every
profile and its avatar reference but not the pictures. If the storage server or
the local volume is lost, the pictures are lost with it. For avatars that is
accepted: people upload them again. Until they do, a profile whose avatar
object is gone shows a broken image where the avatar was (the app answers 404
for it); uploading a new picture replaces the reference and fixes it. A recipe
whose picture object is gone shows its placeholder band instead, never a broken
image, and the app logs "picture object is missing from the media store" with
the key. The local
directory sits on the same volume as the database, so it protects against
nothing that the volume does not.

A job the app was stopped in the middle of, a picture cropped but not yet
processed, runs again when `gobbler serve` starts; the original upload is kept
until processing succeeds.

### Garage as the S3 endpoint for backups and media

The deployment plans to run [Garage](https://garagehq.deuxfleurs.fr/) on a second
machine on the home network, because MinIO is no longer maintained
([ADR-00033](./adr/00033-media-object-storage.md)). Garage speaks plain S3, so
the same endpoint and key serve the backups and the media. Garage has no object
versioning, which neither needs.

The commands below are from memory of Garage's command line and are
**unverified**; follow Garage's own quick start for the version you install, and
treat this as the shape of the setup, not a script.

```sh
# On the Garage machine, after its layout is applied:
garage bucket create gobbler-backups     # unverified
garage bucket create gobbler-media       # unverified
garage key create gobbler                # prints the access key id and the secret
garage bucket allow --read --write gobbler-backups --key gobbler   # unverified
garage bucket allow --read --write gobbler-media --key gobbler     # unverified
```

Then in `.env` on the gobbler host:

```sh
GOBBLER_S3_ENDPOINT=http://garage.lan:3900   # the S3 API port in Garage's config
GOBBLER_S3_REGION=garage                     # must equal s3_region in Garage's config
GOBBLER_S3_BUCKET=gobbler-backups
GOBBLER_S3_MEDIA_BUCKET=gobbler-media
GOBBLER_S3_ACCESS_KEY_ID=...
GOBBLER_S3_SECRET_ACCESS_KEY=...
```

`http://` is fine on a trusted home network; use `https://` if the traffic
crosses anything else. Restart gobbler, upload an avatar from the profile page,
and check the object appears under `people/` in the media bucket.

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
