# ADR-00002: Single Go binary: chi, templ, HTMX, SQLite

**Status:** Accepted

**Date:** 2026-10-06

## Context

gobbler is a personal recipe manager for one household, run on a single Ubuntu
VM and reached through a Cloudflare tunnel. It has to be cheap to operate,
trivial to back up, and pleasant to use on a phone in a kitchen. There is no
team to staff a separate frontend build, and no load that justifies a database
server.

Options considered:

- **TypeScript full-stack (SvelteKit + Drizzle + SQLite).** Rich client
  interactivity, but two runtimes' worth of tooling and a Node dependency tree
  to keep patched on a box that should be left alone. Rejected by the owner in
  favour of Go.
- **Python (FastAPI + HTMX/Jinja).** Comparable shape to the chosen stack, but
  ships an interpreter and a virtualenv rather than one static binary.
- **Go + HTMX with server-rendered HTML.** One statically linked binary,
  interactivity via HTML fragments, no client build step beyond vendoring
  `htmx`. Chosen.

For storage, Postgres was rejected: a second service to run, upgrade and back
up, for a single-user data set measured in megabytes. SQLite is one file, and
`VACUUM INTO` gives a consistent online snapshot (see ADR-00010).

The SQLite driver is `modernc.org/sqlite`, a pure-Go translation. `mattn/go-sqlite3`
was rejected because it needs cgo, which complicates cross-compilation and the
container build for no benefit at this scale.

## Decision

gobbler is one Go binary serving server-rendered HTML.

- **Routing:** `chi`.
- **Templates:** `templ` components, compiled to Go; type-checked at build.
- **Interactivity:** HTMX, vendored and embedded in the binary. Small amounts of
  plain JavaScript only where HTMX cannot reach (cook-mode timers, the Screen
  Wake Lock API).
- **Storage:** SQLite via `modernc.org/sqlite`, in WAL mode. Schema changes are
  `goose` migrations embedded in the binary and applied at start-up.
- **Search:** SQLite FTS5 over recipe name, description, ingredients, tags and
  notes.
- **Static assets:** embedded with `embed`; nothing is loaded from a third-party
  origin at runtime.
- **Packaging:** a container image run by Docker Compose on the VM, with the
  database file on a named volume. `cloudflared` runs alongside as its own
  service.
- **Layout:** `cmd/gobbler/` for the entry point, `internal/<package>/` for all
  other code.

## Consequences

One artefact to build, one file to back up, one process to watch. The price is
that rich client-side behaviour is harder than in a SPA; anything that needs it
must justify the JavaScript.

**Obligations:**

1. The production image contains a single `gobbler` binary plus `tesseract`
   (ADR-00006) and nothing requiring cgo in the Go build.
2. `docker compose up` on a fresh Ubuntu host with only a `.env` file brings
   the app up with migrations applied.
3. No page references a script, stylesheet or font from a third-party origin.
4. Every schema change is a `goose` migration; the binary refuses to start if a
   migration fails.
