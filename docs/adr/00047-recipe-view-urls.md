# ADR-00047: A recipe's view page lives at /recipes/{id}-{slug}

**Status:** Accepted

**Date:** 2026-11-07

## Context

No ADR records the URL of a recipe's view page. It has been `/recipes/{id}`,
which is stable but says nothing when pasted into a message or read in a
browser history. The owner wants readable, shareable recipe URLs, with the id
staying the only way a recipe is found: a title changes, an id does not.

The sub-pages of a recipe (edit, picture, journal, cook, history, clone,
delete) are path segments after the id, so any scheme that puts the slug in
its own segment competes with them for the same position.

Options considered:

- **`/recipes/{id}/{slug}`.** The slug shares a segment position with the
  sub-pages. A recipe titled "Cook" would route to cook mode, so the scheme
  needs a reserved-word list for as long as sub-pages are added.
- **`/recipes/{id}/r/{slug}`.** No collision, but `r` is noise, reads as a
  sub-resource and reserves `r` against future sub-pages.
- **`/r/{slug}` with a lookup by slug.** Needs a unique stored slug per
  household, a migration, duplicate-title handling and a rename story, and it
  gives up lookup by id alone.

## Decision

The view page is `/recipes/{id}-{slug}`, one path segment, for example
`/recipes/42-chicken-curry`.

1. **The id is the only lookup key.** The slug is derived from the recipe's
   current title when a link is built. It is never stored and never looked up.
2. **Reading the segment.** Leading decimal digits are the id and must be a
   positive `int64`. After them the segment ends, or has `-` followed by
   anything, which is ignored. No leading digits, digits followed by any other
   character, and overflow answer 404.
3. **One canonical form.** A GET or HEAD whose segment is not exactly
   `{id}-{slug}` for the current title (the plain `/recipes/42`, a stale slug
   after a rename, a wrong slug) answers 301 to the canonical URL and keeps the
   query string. The recipe is loaded, and the caller's access checked, before
   the redirect: an id that is unknown or not visible to the caller answers 404,
   so a redirect never discloses a title. An HTMX request gets the same 301;
   the browser follows it and the page swaps in the canonical response.
4. **Sub-pages are unchanged.** `/recipes/{id}/edit`, `/picture`, `/journal`,
   `/cook`, `/history`, `/clone` and `/delete` keep a plain numeric id. The slug
   sits in the segment before the `/`, so it cannot collide with them.
5. **The slug.** Lowercase ASCII; diacritics are stripped to their base letter,
   other non-ASCII characters are dropped as separators; each run of
   non-alphanumerics becomes one `-`; leading and trailing `-` are trimmed; the
   length is capped near 60 bytes, cut at a `-` boundary. A title that leaves
   an empty slug has the canonical form `/recipes/{id}`.
6. **One builder.** A single package, `internal/recipepath`, builds the view
   URL from an id and a title, builds the plain-id base for sub-pages, and
   reads the segment. Every link to the view page, in a template, a redirect or
   output for an agent, goes through it. Where only the id is at hand, the
   builder gives the plain form and the 301 completes it.

## Consequences

- A rename changes the canonical URL. Old links keep working through the 301,
  and nothing is stored that could fall out of step with the title.
- A link built without a title costs one redirect; it is never wrong.
- The permanent redirect is cached by browsers. A cached `/recipes/42` leads to
  the older slug, which redirects again to the current one.
- Chi's `{id}` parameter of the view route now carries the whole segment, so the
  view route must parse it with the segment reader. Sub-page routes keep
  parsing a plain integer.
- The slug uses `golang.org/x/text/unicode/norm`, already in the module graph
  and now a direct dependency.

**Obligations:**

1. `internal/recipepath` is the only place that formats or parses the view
   URL: `rg '"/recipes/" \+|"/recipes/%d"' internal --glob '!*_test.go'`
   finds only sub-page paths and the library path.
2. The view handler answers 404 for an unknown id before it can answer 301, and
   a handler test proves it.
3. `docs/design.md` names the URL scheme where it describes the recipe page.
4. The end-to-end route list names `/recipes/{id}-{slug}` as the view route.
