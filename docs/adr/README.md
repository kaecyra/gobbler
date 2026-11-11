# Architecture Decision Records

Numbered, immutable records of architectural rulings. One decision per file.
The convention itself is [ADR-00001](00001-record-architecture-decisions.md) —
read that first.

Numbers are zero-padded to five digits, never reused for a second decision, and
never changed once assigned. A gap in the sequence is normal — a deleted
record's number stays spent. Accepted records are immutable: supersede or amend them, never rewrite them. Nothing has reached production yet, so until the first production deploy a record found wrong may be corrected in place — same number, same file.

Start a new one by copying [TEMPLATE.md](TEMPLATE.md), and add its row to the
index below in the same commit.

## Index

| ADR | Decision | Status |
|---|---|---|
| [00001](00001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [00002](00002-single-binary-go-stack.md) | Single Go binary: chi, templ, HTMX, SQLite | Amended by [00017](00017-cloudflared-on-the-guest.md), [00021](00021-youtube-video-on-a-recipe.md) |
| [00003](00003-cloudflare-access-identity.md) | Identity from verified Cloudflare Access JWT; single user | Superseded by [00030](00030-google-sign-in.md) |
| [00004](00004-recipe-data-model.md) | Recipe data model: components, ingredient lines, step graph, revisions | Amended by [00022](00022-adopt-the-mockup-design.md), [00025](00025-one-cuisine-per-recipe.md), [00035](00035-reviewing-agent-recipe-edits.md), [00041](00041-author-notes.md) |
| [00005](00005-quantities-and-units.md) | Exact quantities, unit dimensions, density conversion, scaling rules | Accepted |
| [00006](00006-rule-first-ingestion.md) | Rule-first ingestion with confidence-gated LLM fallback and mandatory review | Amended by [00020](00020-ingest-step-graph-gap-fill-review.md), [00034](00034-mcp-server.md), [00053](00053-photo-vision-transcription.md) |
| [00007](00007-llm-provider-interface.md) | LLM provider interface with Claude and Gemini | Amended by [00056](00056-llm-provider-per-task.md), [00057](00057-integration-settings-in-the-database.md) |
| [00008](00008-ingredient-catalog.md) | Canonical ingredient catalog: curated seed plus USDA FoodData Central | Accepted |
| [00009](00009-shopping-list.md) | Shopping list aggregation, pantry, live view, Todoist push | Amended by [00018](00018-households-as-tenants.md) |
| [00010](00010-backups.md) | Nightly SQLite snapshot to S3-compatible storage | Amended by [00033](00033-media-object-storage.md) |
| [00011](00011-v1-scope-and-deferrals.md) | Scope and deferrals | Amended by [00018](00018-households-as-tenants.md), [00021](00021-youtube-video-on-a-recipe.md) |
| [00012](00012-web-subpackage-per-feature.md) | Web layer as a server package plus one subpackage per feature area | Accepted |
| [00013](00013-playwright-e2e-in-node.md) | End-to-end tests with Node Playwright under e2e/ | Accepted |
| [00014](00014-sqlc-for-data-access.md) | Data access through sqlc-generated code | Accepted |
| [00015](00015-immutability-at-the-store-api.md) | History immutability is enforced by the store API, not the database | Accepted |
| [00016](00016-cross-origin-protection.md) | Cross-origin requests are refused by Fetch metadata, with a SameSite=Lax session cookie | Accepted |
| [00017](00017-cloudflared-on-the-guest.md) | cloudflared runs on the VM guest; Compose publishes gobbler on loopback only | Amended by [00027](00027-publish-on-all-interfaces.md) |
| [00018](00018-households-as-tenants.md) | Households are tenants; a household has several members | Amended by [00031](00031-api-keys.md), [00032](00032-profiles.md) |
| [00019](00019-sized-equipment-flag.md) | Equipment carries a hand-set `sized` flag that drives the scaled view's larger-pan hint | Accepted |
| [00020](00020-ingest-step-graph-gap-fill-review.md) | Ingest infers the step graph by rules then LLM, the LLM fills only gaps, and review reuses the editor | Accepted |
| [00021](00021-youtube-video-on-a-recipe.md) | A recipe may carry a YouTube video, played only on click, with its thumbnail stored in SQLite | Amended by [00023](00023-video-from-a-page-embed.md) |
| [00022](00022-adopt-the-mockup-design.md) | Adopt the reviewed mockup as the visual design, with the recipe fields it needs | Amended by [00024](00024-equipment-chips-icons-and-catalog.md), [00025](00025-one-cuisine-per-recipe.md) |
| [00023](00023-video-from-a-page-embed.md) | Ingest falls back to the first YouTube embed on the page when the Recipe markup has none | Accepted |
| [00024](00024-equipment-chips-icons-and-catalog.md) | Equipment shows as chips with icons; the central catalog covers common kitchen equipment | Accepted |
| [00025](00025-one-cuisine-per-recipe.md) | A recipe has at most one cuisine, from a fixed list | Accepted |
| [00026](00026-publish-images-to-ghcr.md) | CI publishes the production image to a public GHCR package; the VM pulls it | Accepted |
| [00027](00027-publish-on-all-interfaces.md) | Compose publishes gobbler on all interfaces of the VM | Accepted |
| [00028](00028-invitation-codes.md) | An invitation is addressed by a random eight-character base62 code | Accepted |
| [00029](00029-public-paths.md) | A fixed set of public paths is served without an identity | Amended by [00030](00030-google-sign-in.md), [00034](00034-mcp-server.md) |
| [00030](00030-google-sign-in.md) | The app signs people in over OpenID Connect, with Google; Cloudflare Access is removed | Amended by [00031](00031-api-keys.md), [00032](00032-profiles.md) |
| [00031](00031-api-keys.md) | Personal and household API keys authenticate the machine-facing surface | Amended by [00034](00034-mcp-server.md) |
| [00032](00032-profiles.md) | Everyone has a profile, and one visibility rule decides what a viewer sees | Accepted |
| [00033](00033-media-object-storage.md) | Uploaded media is processed to WebP and kept in S3-compatible object storage | Accepted |
| [00034](00034-mcp-server.md) | gobbler serves MCP, authenticated by API keys or OAuth, with recipe writes held for review | Amended by [00036](00036-rate-limits-on-public-oauth-endpoints.md) |
| [00035](00035-reviewing-agent-recipe-edits.md) | Recipes awaiting review are stored as proposals, and an edit is reviewed in the recipe editor | Amended by [00042](00042-reimport.md) |
| [00036](00036-rate-limits-on-public-oauth-endpoints.md) | The public OAuth endpoints are rate-limited per client address, and unused client registrations have a ceiling | Accepted |
| [00037](00037-notes-are-editable.md) | A note is a household scratchpad: its body is edited in place and it can be deleted | Accepted |
| [00038](00038-editor-morphs-the-server-rendered-form.md) | The recipe editor morphs the server-rendered form in place | Accepted |
| [00039](00039-recipe-pictures.md) | A recipe's picture is chosen from its source page, kept as media, and shown on cards and the recipe header | Amended by [00040](00040-loopback-fetches-under-the-dev-bypass.md), [00042](00042-reimport.md), [00050](00050-picture-candidates-fall-back-to-1x.md), [00051](00051-edit-page-picture-picker.md), [00052](00052-draft-candidates-chosen-by-id.md) |
| [00040](00040-loopback-fetches-under-the-dev-bypass.md) | The fetch guard admits loopback only while the dev auth bypass is on | Accepted |
| [00041](00041-author-notes.md) | A recipe keeps its author's notes as a short list that follows the steps | Amended by [00044](00044-author-note-length-flagged.md), [00045](00045-author-notes-apart-from-steps.md) |
| [00042](00042-reimport.md) | A saved recipe with a source URL is re-imported through ingestion's review screen, and saving writes a new revision | Accepted |
| [00043](00043-discrete-api.md) | Operations live in an in-process service layer under internal/api, called by the web handlers and the MCP tools | Accepted |
| [00044](00044-author-note-length-flagged.md) | An author note's length is flagged on review, never refused | Accepted |
| [00045](00045-author-notes-apart-from-steps.md) | Author notes sit apart from the step chain, with no connector lines | Accepted |
| [00046](00046-markdown-in-steps.md) | Step text and author notes render a safe markdown subset | Accepted |
| [00047](00047-recipe-view-urls.md) | A recipe view page lives at /recipes/{id}-{slug}; the id alone is the lookup | Accepted |
| [00048](00048-agent-driven-import.md) | Agents preview imports, revise held drafts against a household house style, and leave one review per recipe | Accepted |
| [00049](00049-pending-drafts-listed-and-counted.md) | Pending import drafts are listed on the import page and counted in the menu | Accepted |
| [00050](00050-picture-candidates-fall-back-to-1x.md) | Picture candidates prefer 2x and fall back to 1x when none meets it | Accepted |
| [00051](00051-edit-page-picture-picker.md) | A saved recipe's picture is chosen on its edit page, from a search of its source page or an upload | Accepted |
| [00052](00052-draft-candidates-chosen-by-id.md) | A draft's picture candidates are chosen by an ID of their own | Accepted |
| [00053](00053-photo-vision-transcription.md) | A photo is transcribed by a vision model, structured by the rules, and checked for numbers against tesseract | Accepted |
| [00055](00055-invitation-email.md) | Invitations are emailed over SMTP with TLS, as HTML with a plain-text part | Accepted |
| [00056](00056-llm-provider-per-task.md) | Text and vision each name their own LLM provider, set on the operator's page and stored in the database | Accepted |
| [00057](00057-integration-settings-in-the-database.md) | Integration settings live in the database, and their secrets are encrypted there under a key from the environment | Accepted |
| [00058](00058-llm-usage-and-cost.md) | Every LLM call is recorded with its cost, priced from a daily OpenRouter sync with operator overrides, and shown on an operator page | Accepted |

## What needs one

Anything expensive to reverse, or that a later reader would otherwise have to
reconstruct: schema shape, data ownership, isolation boundaries, authorization
models, and what is deliberately deferred. A recorded deferral is a decision,
not an omission. Implementation detail inside an agreed design does not need
one.

An ADR is also what moves work from uncovered to covered — see
[collaboration](../../.claude/rules/collaboration.md). Until the decision is
written down, the work it authorises is still a conversation.

## Making a decision

One decision per exchange. Verify the claims against the code first, explain
the situation in plain English, argue the options honestly including the
rejected ones, and record the outcome immediately.

Verifying first is not ceremony. Open questions are usually written from
memory, and memory drifts — a decision made on a "fact" nobody checked is a
decision made about a system that does not exist.

Recording immediately is not either. Commit each ADR as it is made, not at the
end of a pass: a session ending with a dozen rulings in an uncommitted file has
produced nothing durable. Where the consequences are large, the ADR lands first
and the consequential edits follow in their own commit. The record is the part
that must not be lost.
