# ADR-00043: Operations live in an in-process service layer under internal/api, called by the web handlers and the MCP tools

**Status:** Accepted

**Date:** 2026-11-06

## Context

The owner asked for the work behind the web handlers to be split into a
discrete API, which the same binary can expose for now. The decisions below
were made one at a time with the owner; the working record, with the evidence
checked against the code, is `docs/proposals/discrete-api.md`.

There is no service layer. The HTMX handlers (one subpackage per feature,
ADR-00012) and the MCP tools (ADR-00034) each call the domain packages
directly. 21 non-test files under `internal/web/` import `store`, and
`internal/mcp/` imports `store`, `ingest`, `shopping`, `recipe`, `catalog`,
`household` and others. So an operation such as deleting a recipe or adding to
a shopping list is written once per door, and the two copies can disagree on
membership checks, validation and error handling. That drift is what prompted
the request. Two authentication routes reach those doors: the session cookie
from Google sign-in (ADR-00030) and API keys or OAuth grants for MCP
(ADR-00031, ADR-00034). `internal/picture` already has the shape wanted: the
web picture handlers call its domain functions, and the household delete
reaches it through an interface.

Options considered for the form of the API:

- **An HTTP/JSON API the handlers call over the network, now.** Rejected: every
  page render becomes an internal HTTP call, with authentication for the web's
  own calls, versioning and JSON shapes to keep, for one binary. The owner also
  named scaling as a benefit. Everything sits on one SQLite file with a single
  writer, so more API processes add network hops and add no write capacity.
  Scaling out would mean replacing SQLite, which is its own decision.
- **Leave the handlers and MCP tools calling the domain packages directly.**
  Rejected: nothing to build, and the drift continues.
- **An in-process service layer, with a JSON API added later as a thin adapter
  when a non-browser client exists.** Chosen. It gives one home per operation
  and cleaner handlers now, and makes a network API a small step later.

Options considered for how the layer is divided:

- **One service package per web screen**, mirroring ADR-00012. Rejected:
  deleting a recipe would be spread across screens, and MCP would have to pick
  one of them, which recreates the drift.
- **Operations inside the existing domain packages** (`store`, `shopping`,
  `household`). Rejected: no new tree, but `store`, already the largest
  package, would take orchestration and membership checks on top of
  persistence.
- **One package per resource.** Chosen: the recipe view, editor, history,
  journal and cook screens all act on a recipe, and MCP acts on it through
  another door.

Options considered for the migration:

- **One sweep.** Rejected: it rewrites most of `internal/web/` and
  `internal/mcp/` at once, collides with every node in flight and is too large
  to review.
- **Opportunistic**, new code through the layer and old code when touched.
  Rejected: it never finishes, and the drift continues in code nobody touches.
- **One resource per plan node, each fenced by a lint rule.** Chosen.

On the package name: `api` is a stretch against code-style's rule to name a
package for what it provides, because it suggests a network API and the layer
is in-process. The name is kept because the owner's request was for an API and
the later adapter would expose exactly these operations. The package doc of
`internal/api` states that it is in-process. The subpackages are named for
their resource, as the rule asks.

## Decision

- Every operation behind the web handlers lives once, in a service package
  under `internal/api/<resource>`: `recipes`, `shopping`, `households`,
  `catalog`, `journal`, `ingest` and so on. The web handlers and the MCP tools
  call these packages and do not call `store` or the other domain packages
  directly.
- A service operation takes a verified caller (the person and household, and
  for MCP the credential), enforces household membership (ADR-00018) and
  returns domain errors. A handler parses the request and renders the result.
  An operator operation (ADR-00018), which reaches across households, checks
  the caller's person against the configured operators instead of membership.
- Authentication stays at the adapters. The web layer verifies the session
  cookie (ADR-00030) and MCP verifies its bearer credential (ADR-00031,
  ADR-00034). The service layer never reads cookies or headers. A later JSON
  API authenticates at its own adapter, under `internal/web/`, by a decision
  made then.
- ADR-00012 stands. Web feature packages stay one per screen, and only their
  insides shrink. Whether `internal/picture` moves under `internal/api/` or
  stays a domain package that `internal/api/recipes` calls is left to the
  recipes migration.
- The MCP server is a caller of `internal/api/<resource>` beside the web
  handlers, and its tools switch resource by resource as each resource
  migrates.
- Each resource migrates in its own plan node. The node creates
  `internal/api/<resource>`, moves that resource's operations in, switches the
  web handlers and MCP tools to it, and adds a golangci-lint `depguard` rule
  that stops `internal/web/` and `internal/mcp/` importing that resource's
  domain packages directly. Shopping is the pilot because both doors call it,
  so it sets the service signature, the caller type, the error mapping and the
  shape of the rule. Recipes follow.
- Once a resource's package exists, new operations on that resource go there.
  Work already decided for recipes, such as re-import (ADR-00042), is built in
  `internal/api/recipes` if that package has landed, and otherwise moved there
  by the recipes node.

## Consequences

ADR-00012 is unchanged and ADR-00034 gains a layer between its tools and the
domain packages; neither is edited. The tree carries both patterns until the
last resource migrates, and the `depguard` rules mark which resources are done.
No network API exists after this ADR, and adding one needs its own decision on
authentication and versioning.

**Obligations:**

1. Plan node l-discrete-api (#433) adds this ADR and its index row in
   `docs/adr/README.md`, and appends a paragraph on `internal/api/` to the
   package map in `docs/design.md`.
2. A shopping pilot node creates `internal/api/shopping` with a package doc
   stating it is in-process, moves the shopping operations in, switches the
   shopping web handlers and MCP tools to it, and adds a `depguard` rule to
   `.golangci.yml` (which enables none today) that fails `make lint` when
   `internal/web/` or `internal/mcp/` imports `shopping` or the shopping
   queries of `store` directly.
3. A recipes node does the same for `internal/api/recipes`, and settles where
   `internal/picture` sits.
4. The remaining resources each get a node. The last one leaves
   `internal/web/` and `internal/mcp/` with no direct import of `store`,
   `shopping`, `household` or `catalog` outside `internal/api/`.
5. Each migration node switches the MCP tools for its resource in the same pull
   request as the web handlers.
