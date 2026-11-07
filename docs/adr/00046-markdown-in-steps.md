# ADR-00046: Step text and author notes render a safe markdown subset

**Status:** Accepted

**Date:** 2026-11-07

## Context

Step text and author-note text are stored as plain strings and rendered
escaped, so a recipe cannot bold a warning, link a technique or break a step
into a short list. On 2026-11-07 the owner asked for markdown, including line
breaks and block styles.

Step text reaches the page from scraped pages, LLM output (ADR-00006,
ADR-00034) and people typing, so whatever renders it is an injection boundary.
The page also loads no remote content except the YouTube player (ADR-00021).

Options considered:

- **Inline only** (emphasis, links, inline code), with a renderer written for
  the purpose and no new dependency. Proposed first; the owner wanted line
  breaks and block styles too.
- **Full CommonMark.** Headings would break the page's heading order, images
  would be remote loads, tables do not fit a step card, and raw HTML is an
  injection path. Rejected.
- **A CommonMark parser restricted to an allowed set of elements.** Chosen.
  Writing a block parser by hand is not worth it, and an established parser
  with raw HTML switched off keeps the boundary in one audited place.
- **Converting a source page's own formatting into markdown on ingest.**
  It would change ingestion's drafts and needs its own parser-version bump.
  Not taken now; ingest output stays plain text, which is valid markdown.

## Decision

- Step text and author-note text are markdown. They are stored as typed: the
  source string is the record, and the rendered HTML is never stored.
- The recipe page and cook mode render them as HTML through one renderer in
  the codebase. It allows paragraphs, hard line breaks (a single newline
  breaks the line), bulleted and numbered lists, blockquotes, emphasis, strong
  emphasis, inline code and links. Everything else markdown can express,
  including headings, images, tables, code blocks, horizontal rules and raw
  HTML, is rendered as its escaped source text, never as markup.
- A link renders only for an `http` or `https` destination, and carries
  `rel="nofollow noopener noreferrer"`. Any other scheme renders as text.
- Surfaces that are plain text by nature (the MCP tools, the shopping-list text
  export, page titles and meta descriptions) carry the markdown source
  unchanged.
- The editor and the ingest review screen edit the source in a plain textarea.

## Consequences

Step and note text written before this change renders as before, except that
markdown punctuation such as a leading `- ` or `1. ` now forms a list and a
pair of `*` now forms emphasis. The project holds no data anyone keeps, so no
migration escapes it.

The renderer is a new module dependency, pinned like every other, with its
version checked against the module proxy when added.

**Obligations:**

1. One renderer function renders step and author-note text on the recipe page
   and in cook mode; no surface renders either with a second implementation.
2. A test feeds the renderer `<script>`, an `<img>` tag, a `javascript:` link,
   a heading, an image and a table, and asserts none of them becomes an
   element.
3. `docs/ui/design.md` states how the allowed block elements look inside a
   step and an author note.
4. `docs/adr/README.md` lists this record.
