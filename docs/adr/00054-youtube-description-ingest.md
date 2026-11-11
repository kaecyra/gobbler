# ADR-00054: A YouTube link is read from the video's description, or from the recipe page it links, through the YouTube Data API

**Status:** Accepted

**Date:** 2026-11-11

## Context

A cook who pastes a YouTube link today gets the watch page, which carries no
recipe, so the draft is empty. The recipe is almost always in the video's
description, or at a blog the description links to.
[ADR-00021](00021-youtube-video-on-a-recipe.md) keeps the video on a recipe;
[ADR-00006](00006-rule-first-ingestion.md) and
[ADR-00053](00053-photo-vision-transcription.md) say how a source reaches the
rule parser.

The owner ruled on 2026-11-10 that the recipe comes from the description or a
linked page with a schema.org Recipe; there is no transcript and no vision
model; Instagram is paste-only; TikTok is out. On 2026-11-11 the owner ruled
that the description is fetched with the YouTube Data API and its key is set on
the operator's Deployment page.

Options for getting the description:

- **Scrape the watch page.** No key. Rejected: the description sits in
  script-embedded player data that YouTube changes without notice, and a
  scraper is the kind of dependency that fails silently.
- **A transcript.** Rejected by the owner: spoken recipes are loose, and it
  needs an audio or caption path.
- **The Data API's `videos.list` with `part=snippet`.** Chosen. It costs 1 unit
  of a 10,000-unit daily default quota per call, far above a household's use.

Options for the key:

- **An environment variable.** The issue's first position. Superseded by the
  owner: an operator would need shell access to set or rotate it, the problem
  [ADR-00057](00057-integration-settings-in-the-database.md) solves.
- **An integration secret on the Deployment page.** Chosen.

Options for turning a description into a draft:

- **Send it to the LLM to structure.** Rejected: ADR-00006 keeps structuring
  with the rules.
- **Treat it as pasted text.** Chosen: the same sectioning, parsing, scoring
  and fallback as pasted text, so the LLM is reached only below the threshold
  and its output only through the review screen.

Options for the linked page:

- **Always prefer the description.** Rejected: a blog's markup is exact where
  free text is not.
- **Always prefer the page.** Rejected: a page with no recipe data (a shop, a
  social profile) would replace a usable description.
- **Prefer the first linked page that carries structured recipe data, else the
  description.** Chosen.

## Decision

1. **The key is an integration secret.** This amends
   [ADR-00057](00057-integration-settings-in-the-database.md)'s scope: the
   integration settings that live in the database, edited on the Deployment
   page and sealed under the environment's encryption key, are mail, the LLM
   and the YouTube Data API key (integration `youtube`, secret field
   `api_key`). Nothing else of ADR-00057 changes. The key is read on every
   lookup, so a change takes effect on the next import.
2. **No key, no lookup, no error.** A YouTube link is then ingested as it was:
   the page is fetched like any URL. A saved key that cannot be opened (wrong
   or missing encryption key) is the same off state, and is logged at error
   level each time an import finds it.
3. **With a key, a YouTube video link (watch, short, shorts or embed form, not
   a bare ID) is looked up before any page is fetched.** The video's title and
   description come from `videos.list`. A bare ID, or a link on another host,
   never reaches the lookup.
4. **Linked pages are tried first.** The description's `http` and `https` links
   are taken in order of appearance, without repeats and without YouTube's own
   hosts, up to three. Each is fetched through the URL-ingest fetcher with all
   its limits (guarded client, timeout, size cap, redirect limit); any host is
   allowed. The first whose page yields structured recipe data (JSON-LD,
   microdata or a site extractor, not the text fallback) is drafted exactly as
   a URL import of that page. A link that cannot be fetched is logged and the
   next is tried.
5. **Otherwise the description is the source.** The title, a blank line and the
   description are the text the rules read, as pasted text. A video with no
   description is an error, not an empty draft.
6. **The snapshot is the input the rules read.** A linked page is a snapshot of
   kind `html`, with the page's own address as the source URL. The description
   is a snapshot of kind `text`, with the YouTube link as the source URL; the
   text is the title and description as the rules saw them, so a replay
   reproduces the draft. No snapshot kind is added. The snapshot is written at
   save and never modified.
7. **The video stays on the recipe.** The draft's video is the one the person
   linked, unless the linked page's own markup names a video, which then wins
   as in [ADR-00023](00023-video-from-a-page-embed.md). Like every drafted
   field it is shown on the review screen.
8. **Re-import repeats the flow** ([ADR-00042](00042-reimport.md)). A recipe
   from a description has the YouTube link as its source URL, so re-import
   looks the video up again. A recipe from a linked page has that page as its
   source URL and re-imports it as any page; because the page may not name the
   video, a re-import keeps the recipe's video when the page names none.
9. **Failures are errors the person can read, never an empty draft.** A used-up
   quota, a key YouTube rejects, a private or removed video, and a transport
   failure or unreadable answer each fail the import with a message saying
   which; it answers as a fetch failure does. The log gets a warning with the
   video ID and the API's reason. The key is never in either: the request
   carries it in the query string, so the transport error's URL is dropped and
   any error text is scrubbed of the key before it is wrapped. No fallback to
   the watch page, which would produce the empty draft this record exists to
   avoid.
10. **The agent endpoint is a follow-up.** The MCP import shares the pipeline
    but is not given the lookup, so an agent's YouTube link keeps the old path
    (the watch page) until the lookup is wired there. That is deliberate and
    unfinished, not a ruling that agents should not get descriptions.

## Consequences

The pipeline gains a second reason to fetch a page and a first reason to call a
service other than the LLM before the rules. The rule-first boundary holds: the
description is stored text the rules structure, the LLM runs only where it does
for pasted text, and nothing is saved without the review screen. The AGENTS.md
secrets boundary names the key.

A draft from a description depends on what YouTube returned at import time; the
snapshot is what makes it reproducible afterwards.

**Obligations:**

1. `internal/video` tests, over a fake Data API, show the request carries
   `part=snippet`, the video ID and the key; and map a used-up quota, a
   rejected key, a missing video and an empty item list each to its own error,
   with no key in any error string including a refused connection's.
2. `internal/ingest` tests show: the description drafted as `text` under the
   YouTube link with the video kept; a linked page with a Recipe winning, as
   `html` under the page's address; a linked page without one falling back to
   the description; at most three links, no repeats, no YouTube links; a failed
   link not stopping the next; no key leaving the page fetch as it was and
   logging nothing; each failure an `ErrFetch` that names its cause and logs;
   a video with no description an error; and re-import keeping the video.
3. A `cmd/gobbler` test with a real settings store shows the lookup is off with
   no key, uses the key once saved, is off again once cleared, and is off with
   an error log, never the key, when the key cannot be opened.
4. `internal/web/settings` tests show the YouTube section is the key and
   nothing else, and that the saved key is never on the page.
5. An end-to-end test saves the key on the Deployment page and shows a
   description draft, a linked-page draft, a quota failure and a removed video
   against a fake Data API, with no request to YouTube.
6. ADR-00057's Status line and a note at its "What moves, and what stays" name
   this ADR; the `docs/adr/README.md` rows for both say so.
7. `AGENTS.md` names the YouTube Data API key among the integration secrets;
   `docs/operations.md` explains how to get and save the key and gains
   `GOBBLER_VIDEO_API_HOST`; `docs/design.md`'s configuration table and
   `video` package row describe the lookup.
8. The lookup is its own dependency, not part of the LLM provider:
   `web.Deps.Descriptions` reaches `ingest.Deps.Descriptions` through the ingest
   routes and service, and `cmd/gobbler` sets it. The MCP import is not given
   it (decision 10); wiring it there is tracked as a follow-up issue naming
   `internal/mcp`.
