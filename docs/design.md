# gobbler design

How gobbler fits together, and the order it gets built in. Each decision has its
reasoning in an ADR in [`docs/adr/`](adr/README.md); this document links to them
rather than restating them. Where this document and an ADR disagree, the ADR wins
and this document is wrong.

## What it does

- **Ingest** a recipe from a URL, pasted text or a photo, using rules first and
  an LLM only as a fallback, always through a review screen
  ([ADR-00006](adr/00006-rule-first-ingestion.md)).
- **Store** it as structured data: components, semantic ingredient lines,
  equipment, a step graph, times, notes, revisions
  ([ADR-00004](adr/00004-recipe-data-model.md)). A recipe also has an optional
  meal type, cuisine and spice level, a step an optional stage kind and title,
  and a note may be pinned as a tip
  ([ADR-00022](adr/00022-adopt-the-mockup-design.md)); the cuisine is one value
  from a fixed list ([ADR-00025](adr/00025-one-cuisine-per-recipe.md)).
- **Watch** it, optionally: a recipe may carry a YouTube video that plays at the
  top of the steps column only when someone presses Play; the thumbnail is
  downloaded at save and kept in SQLite ([ADR-00021](adr/00021-youtube-video-on-a-recipe.md)).
- **Find** it by full-text search and tags.
- **Follow** it: scale it, switch units, see "meanwhile" guidance, use cook mode
  ([ADR-00005](adr/00005-quantities-and-units.md)).
- **Shop** for it: merged, aisle-grouped lists with pantry exclusion, a live
  phone view and text export ([ADR-00009](adr/00009-shopping-list.md)).
  Todoist push is deferred ([ADR-00018](adr/00018-households-as-tenants.md)).

## Runtime

```
browser ──► cloudflared ──► gobbler (Go binary) ──► SQLite file
                                 │
                                 ├──► OpenID Connect issuer (sign-in)
                                 ├──► recipe sites (URL ingest)
                                 ├──► tesseract (local OCR)
                                 ├──► Claude / Gemini APIs (fallback only)
                                 └──► S3-compatible storage (backups)
```

Docker Compose runs `gobbler` on the Ubuntu VM and publishes its port on
all interfaces ([ADR-00027](adr/00027-publish-on-all-interfaces.md)). `cloudflared` runs on the VM guest, outside Compose, and reaches
it at `http://127.0.0.1:8080`. gobbler verifies identity itself on every
request, from its own sign-in session
([ADR-00030](adr/00030-google-sign-in.md)). The stack is
[ADR-00002](adr/00002-single-binary-go-stack.md).

## Package map

All application code except the entry point lives under `internal/`;
Go test tooling for the end-to-end suite lives under `e2e/`
(ADR-00013). Arrows point at
dependencies.

| Package | Owns | Depends on |
|---|---|---|
| `cmd/gobbler` | Entry point, wiring, subcommands (`serve`, `migrate`, `import-usda`, `backup`) | everything below |
| `config` | Environment-driven configuration, validated at start-up | — |
| `db` | SQLite connection, WAL setup, embedded `goose` migrations, the shared transaction helpers (`WithTx`, `WithReadTx`) and constraint-error wrapper (`WrapWrite`) | `config` |
| `auth` | The identity middleware: the sign-in session cookie, or the dev bypass on a loopback listener | `config`, `session` |
| `session` | Server-side sign-in sessions named by a random cookie token, stored as its SHA-256; queries in `queries/`, sqlc-generated `sessiondb` ([ADR-00030](adr/00030-google-sign-in.md)) | `db` |
| `profile` | A person's profile keyed by email (display name, avatar reference) at an opaque random address; the one visibility rule keyed on the viewer's relation (`Visible`); the default avatar drawn from a hash of the email (`NewIdenticon`); queries in `queries/`, sqlc-generated `profiledb` ([ADR-00032](adr/00032-profiles.md)) | `db` |
| `apikey` | Personal and household API keys: a random secret shown once, stored as its SHA-256; lookup by secret (`Lookup`) for the MCP endpoint, and the write log that records which key made each MCP write; queries in `queries/`, sqlc-generated `apikeydb` ([ADR-00031](adr/00031-api-keys.md)) | `db` |
| `mcp` | The MCP server at `/mcp` over Streamable HTTP, on the official Go SDK ([ADR-00034](adr/00034-mcp-server.md)): the bearer check behind a `Verifier` interface (API keys today), the per-credential leaky-bucket rate limit, paged list tools, and the recipe, import and shopping tools. Recipe writes become drafts for the review screen; shopping writes apply and are recorded against the credential | `apikey`, `store`, `shopping`, `catalog`, `api/ingest`, `recipe/display`, `household` |
| `media` | Uploaded media (ADR-00033): the storage interface with S3 and local-directory implementations, and the image pipeline that decodes JPEG, PNG, GIF, WebP, HEIC and AVIF, crops, bounds to 1024 by 1024 and encodes metadata-free lossy WebP (pure Go, no cgo) ([ADR-00033](adr/00033-media-object-storage.md)) | `config` |
| `oauth` | The OAuth 2.1 authorization server for MCP clients ([ADR-00034](adr/00034-mcp-server.md)): client registration and Client ID Metadata Document fetching (HTTPS only, never a private address), authorization codes with PKCE, per-person grants, rotating refresh tokens stored as hashes, and the access-token verifier `/mcp` uses; served by `web/oauth`, with grants listed on the profile by `web/profile`; queries in `queries/`, sqlc-generated `oauthdb` | `db` |
| `quantity` | Rationals, ranges, units, dimensions, conversion, scaling (pure) | — |
| `recipe` | Domain types (with the closed sets of meal types, cuisines and stage kinds), step-graph validation, "meanwhile" derivation (pure) | `quantity`, `video` |
| `video` | Reading a pasted YouTube link into the video ID a recipe stores, the thumbnail and embed addresses, and the thumbnail download ([ADR-00021](adr/00021-youtube-video-on-a-recipe.md)) | — |
| `recipe/display` | What pages show of a recipe: scale, servings and unit-mode options, formatted ingredient and equipment lines, step numbers, durations, meanwhile items; shared by the recipe view and cook mode, which may not import each other (pure apart from catalog lookups) | `quantity`, `recipe`, `catalog` |
| `store` | Persistence for recipes, revisions, notes, snapshots, video thumbnails, FTS index; queries in `queries/`, sqlc-generated `storedb` ([ADR-00014](adr/00014-sqlc-for-data-access.md)) | `db`, `recipe`, `video` |
| `catalog` | Canonical ingredients, units, equipment, aliases, matching, USDA import; queries in `queries/`, sqlc-generated `catalogdb` ([ADR-00014](adr/00014-sqlc-for-data-access.md)); ingredients in a central layer plus a per-household overlay, scoped by the household on the context ([ADR-00018](adr/00018-households-as-tenants.md)) | `db`, `household`, `quantity` |
| `parse` | Ingredient-line parser, duration parser, text sectioning (pure) | `quantity`, `catalog` (matching interface) |
| `ingest` | Acquire → extract → parse → score → fallback pipeline; JSON-LD, microdata, site extractors, OCR | `parse`, `recipe`, LLM interface |
| `llm/claude`, `llm/gemini` | Provider implementations ([ADR-00007](adr/00007-llm-provider-interface.md)) | provider SDKs |
| `shopping` | List generation, merging, pantry, check state, text export | `quantity`, `recipe`, `catalog` |
| `sharing` | Sending a copy of a recipe to another household and accepting or declining the offers received ([ADR-00018](adr/00018-households-as-tenants.md)) | `household`, `catalog`, `shopping`, `store` |
| `backup` | Nightly `VACUUM INTO`, S3 upload, retention ([ADR-00010](adr/00010-backups.md)) | `db`, `config` |
| `web` | Server, chi router, middleware (including the bearer path that keeps `/mcp` off the session), health check, static assets; `web/layout` shared templ layout; `web/sharing` the share inbox and send form; `web/signin` OpenID Connect sign-in, its callback and sign-out ([ADR-00030](adr/00030-google-sign-in.md)), with `web/signin/signintest` the fake issuer its tests sign in against; one `web/<area>` package per feature with its handlers and templ components ([ADR-00012](adr/00012-web-subpackage-per-feature.md)) | all domain packages |

`api/<resource>` packages ([ADR-00043](adr/00043-discrete-api.md); `api/shopping` is the first) hold each resource's
operations once, in process. The web handlers and the MCP tools call them, take no other route to
`store` and the domain packages, and hand them a verified caller; the service enforces household
membership. Authentication stays in `auth`, `web` and `mcp`. Resources migrate one at a time, each
fenced by a `depguard` rule.

`api/shopping` is the pilot and sets the pattern; its package doc is the
specification. A `Service` method takes `ctx`, then a `Caller` (verified email
and household), then its arguments. On every call it looks the person up in the
household store (`Members.ForEmail`) and refuses with `ErrNotMember` (403 on the
web) unless they belong to the Caller's household. A household API key has no
person, so its Caller sets `HouseholdKey` and is admitted by its household alone
(ADR-00031). The method runs in that household and returns domain errors, which
`web/layout`'s `statusTable` maps for the web and `mcp/tools.go` maps for MCP.
It re-exports the domain types and errors as aliases so adapters import nothing
from `shopping`.
The `shopping-through-api` rule in `.golangci.yml` fails `make lint` when a non-test file under
`internal/web/` or `internal/mcp/` imports `internal/shopping` (and so `shoppingdb`). The router and the
MCP endpoint build the store with `apishop.NewStore`, so neither names the domain package.

`api/recipes` is the second resource: it holds the recipe operations (view, save with the stale-version
check, history, restore, clone, delete with the recipe's picture objects, proposals, drafts and their
save, and starting a re-import) for the recipe web packages and the MCP recipe, edit, import and
re-import tools. `Caller` and the membership check are written once in `api/caller`, which both
resources use. `internal/picture` stays a domain package that `api/recipes` calls (the package doc gives
the reasoning). The `recipes-through-api` rule fails `make lint` when a non-test file under
`internal/web/` or `internal/mcp/` imports `internal/store` or `internal/picture`; the files of
resources that have not migrated (the status table)
are excepted by name until #471. The rules read generated files too: golangci-lint
skips them by default, and every `*_templ.go` is generated, so `.golangci.yml` turns that off and
excludes the other linters from templ and sqlc output instead, leaving depguard alone to read them.
The router builds the store, the pictures and the reaper with `apirecipes.NewStore`, `NewPictures` and
`NewReaper`. Fetching and reading a page into a draft stays in `ingest`, a library that `api/ingest` calls
and the adapters do not; the recipe service holds its result.

`api/journal` holds the notes, rating and made-it log of a recipe for `internal/web/journal`, built over the recipe store (the journal has no domain package of its own) with the same `Caller` and membership check.

## Configuration

All configuration comes from the environment, read once by `config`.
Secrets are never in the repository.

| Area | Settings |
|---|---|
| Server | listen address, data directory, log level, dev-auth bypass flag, trusted proxies whose `CF-Connecting-IP` header is believed ([ADR-00036](adr/00036-rate-limits-on-public-oauth-endpoints.md)) |
| OIDC | issuer URL, client ID, client secret, public base URL ([ADR-00030](adr/00030-google-sign-in.md); required unless the dev bypass is on) |
| Session | session lifetime, fixed from sign-in ([ADR-00030](adr/00030-google-sign-in.md)) |
| MCP | requests per second and burst allowed each credential at `/mcp` ([ADR-00034](adr/00034-mcp-server.md)) |
| OAuth | requests per second and burst allowed each client address (an IPv6 one by its /64) at `/oauth/register` and at `/oauth/token` ([ADR-00036](adr/00036-rate-limits-on-public-oauth-endpoints.md)) |
| Ingestion | confidence threshold, fetch timeout, user agent |
| Video | thumbnail host (YouTube's image host unless overridden; the e2e suite points it at a fixture) |
| LLM | provider, text model, vision model, per-provider API key, timeout |
| Backup | schedule, local retention, S3 endpoint, region, bucket, prefix, credentials, remote retention |

## Testing

Pure packages (`quantity`, `recipe`, `parse`, `shopping` merging) are
table-tested. `parse` has a golden-file corpus of real ingredient lines and
whole recipes. `store`, `catalog`, `ingest` and `backup` have integration tests
against a real SQLite file, with HTTP, LLM and S3 mocked at the
boundary. Every route has a Playwright e2e test under `e2e/`
([ADR-00013](adr/00013-playwright-e2e-in-node.md)). Gates are in
[git-workflow](../.claude/rules/git-workflow.md#gates).

## Waves

Each wave depends on the ones before it, except where noted. The plan graph
breaks each wave into nodes.

| Wave | Delivers |
|---|---|
| **W0 Foundation** | Module scaffold, `config`, `db` with migrations, `auth` middleware, base layout and HTMX, health check, Dockerfile and Compose, Makefile gates, lefthook, `VERSION`, CI |
| **W1 Domain core** | `quantity` (rationals, units, conversion, scaling), `recipe` types and step-graph validation, schema and `store` with revisions and snapshots, `catalog` with seed data |
| **W2 Rule ingestion** | `parse` with golden corpus, JSON-LD and microdata extraction, text sectioning, confidence scoring, review screen, save |
| **W3 Recipe UI** | List and search (FTS5, tags), recipe view with scaling and unit modes, editor for components, lines, qualifiers, equipment and step dependencies, revisions and restore, clone, notes, ratings, made-it log, catalog cleanup page |
| **W4 Shopping** | List building from recipes and manual items, pantry, merge and aisle grouping, live view over SSE, text export |
| **W5 LLM and photo** | LLM interface, Claude and Gemini providers, automatic and manual fallback, `tesseract` OCR, vision fallback, USDA import (can run in parallel with W3–W4 after W2) |
| **W6 Cooking** | "Meanwhile" guidance in the recipe view, cook mode with step timers and screen wake lock |
| **W7 Operations and integrations** | Nightly backups to S3 with retention, settings page with backup status, operations doc with deploy and restore steps |
| **W8 Households** | Household membership and admission, `household_id` scoping of recipe and shopping data, the catalog's central layer and per-household overlay, invitations and the operator's pages, recipe sharing by copy, cross-household route tests ([ADR-00018](adr/00018-households-as-tenants.md)) |
| **Later** | Meal planning ([ADR-00011](adr/00011-v1-scope-and-deferrals.md)); Todoist push ([ADR-00018](adr/00018-households-as-tenants.md)) |

`api/sharing` is the sharing resource: send a recipe copy to another household, list the inbox, accept or decline, for `web/sharing`. It wraps `internal/sharing`, which keeps the rules of a share, and the `sharing-through-api` rule fails `make lint` when a non-test file under `internal/web/` or `internal/mcp/` imports `internal/sharing`.
The recipe picture routes (`internal/web/picture`) serve a recipe's picture and a draft's candidates through `api/recipes` (`PictureKey`, `Candidates`), which checks the caller's membership; they no longer import `internal/picture`.
`api/catalog` is the catalog resource: ingredient, equipment and unit reads and the ingredient edits, merge and suggestions, for the catalog page, the recipe editor and views, cook mode and the MCP recipe tools. It wraps `internal/catalog` with the shared `Caller` and membership check, and `Bound` serves the lookups that carry only a context. The `catalog-through-api` rule fails `make lint` when a non-test file under `internal/web/` or `internal/mcp/` imports `internal/catalog` The operator's equipment review, promotions and central ingredient edits are `Operator*` methods of the service, which check the caller's person against the configured operators instead of membership, since an operator need not be in a household. The central ingredient methods also run with no household on the context (`household.WithoutHousehold`), so an operator who is a member edits the central entry rather than their household's overlay.
`api/households` is the household resource: members, invitations, leaving and deleting for `web/household`, and the operator's household invitations for `web/operator`. It wraps `internal/household` with the shared `Caller` and membership check; accepting an invitation needs only a verified person, since the invitee is not yet a member, and the `Operator*` methods check the caller's person against the configured operators. The membership middleware stays in `internal/web` and puts the household on the context, which the service re-exports as `FromContext`. The `households-through-api` rule fails `make lint` when a non-test file under `internal/web/` or `internal/mcp/` imports `internal/household`.
`api/ingest` is the ingestion resource: drafting a recipe from a URL, pasted text or a photo, and the LLM retry, for `web/ingest` and the MCP import and re-import tools. It wraps `internal/ingest` with the shared `Caller` and membership check, and `Bound` serves the seams that carry only a context. The rule-based path still runs first and a draft reaches a recipe only through the review screen (ADR-00006). The `ingest-through-api` rule fails `make lint` when a non-test file under `internal/web/` or `internal/mcp/` imports `internal/ingest` or its `extract` and `ocr` packages.
`api/profile` is the profile resource: the profile page, display name, sessions, personal API keys, connected apps and avatar, for `web/profile` and the header avatar. It wraps `internal/profile`, `internal/session`, `internal/apikey` and `internal/oauth` with the shared `Caller`; these are the person's own data, so every method checks the Caller is a verified person (a household key is refused) and acts on that person alone, with no household membership needed, and `Subject` and `OpenAvatar` show another person's profile only as the visibility rule allows. The `profile-through-api` rule fails `make lint` when a non-test file under `internal/web/` or `internal/mcp/` imports `internal/profile`.
The status table in `internal/web/layout` (`httperr.go`) maps the api packages' re-exported errors to HTTP statuses, not the domain sentinels; only `internal/parse` and `internal/recipe/display`, which no api package re-exports and no rule fences, stay imported there.
