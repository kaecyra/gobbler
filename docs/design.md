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
  ([ADR-00004](adr/00004-recipe-data-model.md)).
- **Watch** it, optionally: a recipe may carry a YouTube video that plays at the
  top of its page only when someone presses Play; the thumbnail is downloaded at
  save and kept in SQLite ([ADR-00021](adr/00021-youtube-video-on-a-recipe.md)).
- **Find** it by full-text search and tags.
- **Follow** it: scale it, switch units, see "meanwhile" guidance, use cook mode
  ([ADR-00005](adr/00005-quantities-and-units.md)).
- **Shop** for it: merged, aisle-grouped lists with pantry exclusion, a live
  phone view and text export ([ADR-00009](adr/00009-shopping-list.md)).
  Todoist push is deferred ([ADR-00018](adr/00018-households-as-tenants.md)).

## Runtime

```
browser ──► Cloudflare Access ──► cloudflared ──► gobbler (Go binary) ──► SQLite file
                                                      │
                                                      ├──► recipe sites (URL ingest)
                                                      ├──► tesseract (local OCR)
                                                      ├──► Claude / Gemini APIs (fallback only)
                                                      └──► S3-compatible storage (backups)
```

Docker Compose runs `gobbler` on the Ubuntu VM and publishes its port on
loopback only. `cloudflared` runs on the VM guest, outside Compose, and reaches
it at `http://127.0.0.1:8080`. gobbler verifies the Access JWT on every request
([ADR-00003](adr/00003-cloudflare-access-identity.md)). The stack is
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
| `auth` | Access JWT verification middleware, dev bypass | `config` |
| `quantity` | Rationals, ranges, units, dimensions, conversion, scaling (pure) | — |
| `recipe` | Domain types, step-graph validation, "meanwhile" derivation (pure) | `quantity`, `video` |
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
| `web` | Server, chi router, middleware, health check, static assets; `web/layout` shared templ layout; `web/sharing` the share inbox and send form; one `web/<area>` package per feature with its handlers and templ components ([ADR-00012](adr/00012-web-subpackage-per-feature.md)) | all domain packages |

## Configuration

All configuration comes from the environment, read once by `config`.
Secrets are never in the repository.

| Area | Settings |
|---|---|
| Server | listen address, data directory, log level, dev-auth bypass flag |
| Access | team domain, application AUD tag |
| Ingestion | confidence threshold, fetch timeout, user agent |
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
