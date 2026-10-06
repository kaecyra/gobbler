# ADR-00012: Web layer as a server package plus one subpackage per feature area

**Status:** Accepted

**Date:** 2026-10-06

## Context

ADR-00002 puts every route, handler and templ component in one `web` package.
The feature work after the foundation is mostly UI — search and list, recipe
view, editor, history, journal, catalog cleanup, ingestion review, shopping,
cook mode, settings — and the plan graph runs these in parallel, one worker per
feature, each owning an exclusive set of paths.

Options considered:

- **One `web` package, lanes drawn by file prefix** (`editor_*.go`,
  `editor_*.templ`). Matches the original package map literally. But every
  unexported helper is visible to every feature, so a worker reaching for one
  edits a file another worker owns, and a prefix glob is one misnamed file away
  from a collision nobody detects until merge.
- **A server package plus one subpackage per feature area.** Go's package
  boundary becomes the ownership boundary: a feature cannot use another's
  unexported code, and its lane is one directory. Chosen.

## Decision

- `internal/web` holds the HTTP server, the chi router, shared middleware and
  the health check. Route registration for every feature is mounted from it.
- `internal/web/layout` holds the shared templ layout and components every
  feature page uses.
- Each feature area is its own package under `internal/web/<area>/`
  (for example `recipes`, `recipeview`, `editor`, `history`, `journal`,
  `catalog`, `ingest`, `shopping`, `cook`, `settings`), owning its handlers and
  templ components. A feature package exposes a constructor that returns its
  routes; it does not import another feature package.
- Static assets stay embedded under `internal/web/static/`.
- Each feature's Playwright tests live in `e2e/<area>/`.

## Consequences

This amends the `web` row of the package map in `docs/design.md`; ADR-00002's
stack decision is unchanged. Something two features both need goes into
`layout` or a domain package, never into one feature imported by another.

**Obligations:**

1. `docs/design.md`'s package map describes `web` as the server package plus
   `internal/web/layout` and one `internal/web/<area>` package per feature.
2. No package under `internal/web/<area>/` imports another package under
   `internal/web/<area>/`.
