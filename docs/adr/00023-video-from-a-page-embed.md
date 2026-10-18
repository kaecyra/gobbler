# ADR-00023: Ingest falls back to the first YouTube embed on the page when the Recipe markup has none

**Status:** Accepted

**Date:** 2026-10-18

## Context

[ADR-00021](00021-youtube-video-on-a-recipe.md) decision 5 has ingest read a
YouTube video only from schema.org Recipe markup. Recipe blogs often embed
their video another way. The owner's first test page
(japan.recipetineats.com, chicken cutlet) is one: its JSON-LD Recipe has no
`video` property, and its video is a plain YouTube iframe in the post body
under a "Watch How To Make It" heading. The draft came back without a video
that the page plainly shows.

Options considered:

- **Keep markup only.** The owner then pastes the video in the editor. The
  owner rejected this: the video is on the page, and missing it is a fidelity
  bug.
- **Only an embed inside the recipe plugin's card** (WPRM, Tasty, Create).
  This is safer against unrelated videos, but misses this page, whose embed
  sits outside the card.
- **The first YouTube embed anywhere on the page.** It catches this page and
  the common blog layout. The risk is picking an unrelated video, such as a
  sidebar or an ad. The review screen already shows every drafted field, so
  a wrong pick is removed there.

## Decision

When the Recipe markup yields no YouTube video, ingest takes the video ID of
the first YouTube embed in the page body, in document order. An embed is an
iframe whose source is a `youtube.com/embed/` or `youtube-nocookie.com/embed/`
URL. Markup still wins when it names a video. Like every drafted field, the
video is shown on the review screen and saved only through it
([ADR-00006](00006-rule-first-ingestion.md)). Embeds on any other host are
still ignored.

## Consequences

This amends ADR-00021 decision 5: markup first, then the first page embed.
Fetching, storage and playback are unchanged.

**Obligations:**

1. Add an amendment note at ADR-00021 decision 5 pointing here, and set
   ADR-00021's index row in `docs/adr/README.md` to "Amended by 00023".
2. A saved-page fixture from the page above ingests with its embed's video ID,
   and a fixture whose markup names a video keeps the markup's ID even when
   the body embeds another.
