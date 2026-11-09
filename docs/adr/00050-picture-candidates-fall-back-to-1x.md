# ADR-00050: Picture candidates prefer 2x and fall back to 1x when none meets it

**Status:** Accepted

**Date:** 2026-11-09

## Context

ADR-00039 ("Choosing") drops a picture candidate too small to fill the card,
with the minimum taken from the approved mockup. The widest card is 533 by 150
CSS pixels, and the minimum was set at 2x: 1066 by 300 source pixels.

Many food blogs serve only pictures around 700 pixels wide, so an import from
them gets no candidate and the recipe has no picture. Measured on bookmarked
pages: the largest candidate on two japan.recipetineats.com pages (a chicken
cutlet and a katsu curry) is 680 by 952, and on feastingathome.com's Asian
cucumber salad it is 780 by 1170. Each would fill the card at 1x.

Options considered:

- **Keep the 2x minimum.** Rejected: most bookmarked sites lose their picture,
  and "No picture" is a worse card than a softer one.
- **Lower the minimum to 1x for every page.** Rejected: on a page that also
  offers a 2x picture, the person would be offered pictures that look soft on a
  high-DPI screen beside one that does not.
- **Prefer 2x, and fall back to 1x only when no candidate meets 2x.** Chosen by
  the owner.

## Decision

1. The size floor is decided per page from every candidate that downloaded and
   decoded. If at least one meets 1066 by 300 (2x), only candidates meeting
   1066 by 300 are kept. If none does, every candidate meeting 533 by 150 (1x)
   is kept. A candidate below 533 by 150 is always dropped.
2. Deduplication by decoded pixels and the cap of 8 apply to the candidates
   that pass the floor, after it is decided, so a 2x picture late in the
   candidate order still sets the floor.
3. Each candidate is downloaded once.
4. The rule applies wherever candidates are found: import and re-import drafts,
   and re-finding candidates from a recipe's saved html snapshot.

Every other rule of ADR-00039 "Choosing" stands: the candidate order, dropping
failures with a log line, and keeping surviving size variants as separate
options.

## Consequences

ADR-00039's minimum-size rule is amended: "the minimum taken from the approved
mockup" now means the card at 2x, falling back to the card at 1x as above. A
picture kept under the fallback is upscaled on a high-DPI screen and looks
softer there than a 2x one.

Every candidate on a page is downloaded before the floor is known, rather than
stopping at the eighth survivor. Candidate lists come from a page's `og:image`
and its recipe's structured data, so they are short.

**Obligations:**

1. The candidate step in `internal/picture` exports the 2x floor as
   `MinWidth`/`MinHeight` (1066/300) and the 1x floor as
   `FallbackMinWidth`/`FallbackMinHeight` (533/150).
2. A table test in `internal/picture` shows: a mix with one 2x candidate keeps
   only the 2x ones; a 2x candidate after more than 8 smaller ones still sets
   the floor; no 2x candidate keeps every 1x-or-better one; all below 1x keeps
   none; the cap of 8 applies after the floor; identical pixels still collapse.
3. ADR-00039 carries an amendment note at its minimum-size rule pointing here,
   its status line names this record, and the ADR index row for 00039 lists it.
