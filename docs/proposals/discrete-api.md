# Open decisions: a discrete API behind the handlers

Owner request, 2026-11-05 (raised during `docs/proposals/recipe-images.md`, D7): split the work
behind the web handlers into a discrete API. The same binary can expose it for now, but it should be
discrete. Architectural. Worked through with `/overseer:decide`; all decisions are below. Ruled in
[ADR-00043](../adr/00043-discrete-api.md), which carries the obligations; the work is plan node
l-discrete-api (#433) and the migration nodes it names.

## Verified facts

- There is no service layer. The web handlers (HTMX, one subpackage per feature, ADR-00012) and the
  MCP tools (`internal/mcp/`, ADR-00034) each call the domain packages directly: `store` is imported
  by 21 non-test files under `internal/web/` (checked 2026-11-06; an earlier count of 32 included tests); `internal/mcp/` imports `store`, `ingest`, `shopping`,
  `recipe`, `catalog`, `household` and others.
- Two authentication routes exist: the session cookie from Google sign-in (ADR-00030) and API keys
  (`internal/apikey`), used by MCP.

## Open

(none; all closed 2026-11-06)

## Decided

## D1 — An in-process service layer now; a JSON API later as a thin adapter over it

**Decided.** Every operation behind the web handlers lives once, in a per-feature service package in
the same binary. Web handlers, MCP tools and any later JSON API call it, and none of them call `store`
or the other domain packages directly. A service operation takes the caller's identity and household,
enforces membership, and returns domain errors; a handler only parses the request and renders the
result. A network API is not built now. When a non-browser client (a phone app, a script) exists, a
JSON API becomes a third thin adapter over the same layer, beside web and MCP.

**Why.** The owner's drivers, in order: one place per operation so web and MCP cannot drift (the
trigger), cleaner handlers, and readiness for a non-browser client. The service layer delivers the
first two directly, and makes the third a small adapter. `internal/picture` already has this shape:
the web picture handlers and the household delete both call its domain functions.

Rejected:
- **An HTTP/JSON API the handlers call over the network, now.** Truly discrete, but every page render
  becomes an internal HTTP call, with auth for the web's own calls, versioning and JSON shapes to keep,
  for one binary. The owner also named scaling as a benefit; it is not one here. Everything sits on one
  SQLite file with a single writer, so more API processes add hops without adding capacity. Scaling out
  would mean replacing SQLite, which is a separate decision.
- **Leave handlers and MCP tools calling the domain packages directly.** Nothing to build, but the
  drift the owner set out to stop continues.

**Obligations:**

1. The discrete-API ADR records the service layer and the later-adapter path.


## D2 — Authentication stays at the adapters; the service receives a verified caller

**Decided.** With no network API built (D1), authentication does not move. The web layer verifies the
session cookie (ADR-00030) and MCP verifies its bearer credential (ADR-00031, ADR-00034); each hands the
service layer a verified caller (person, household, and for MCP the credential). The service layer
enforces household membership (ADR-00018) and never reads cookies or headers. A later JSON API brings
its own authentication at its adapter, decided then.

**Why.** It follows from D1: authentication belongs to the door a request comes through, and
membership to the operation. Stated by the dispatcher and not objected to by the owner.

**Obligations:**

1. The discrete-API ADR states where authentication and membership sit.

## D3 — One service package per resource, under `internal/api/<resource>`

**Decided.** Service packages are keyed by the resource they act on, not by screen:
`internal/api/recipes`, `internal/api/shopping`, `internal/api/households`, `internal/api/catalog`,
`internal/api/journal`, `internal/api/ingest` and so on. Every screen and every MCP tool that acts on
recipes calls `api/recipes`. ADR-00012 stands: web feature packages stay one per screen, and only their
insides shrink. The package doc of `internal/api` says it is in-process, not a network API. A later JSON
adapter would sit under `internal/web/` and call the same packages. Whether `internal/picture` moves
under `api/` or stays a domain package that `api/recipes` calls is left to the migration.

**Why.** Recipe view, editor, history, journal and cook all act on a recipe, and MCP acts on the same
things through another door. Keying the packages by resource gives each operation one home, which is
D1's point.

Rejected:
- **One service package per web screen**, mirroring ADR-00012. Familiar, but "delete a recipe" would be
  spread across screens and MCP would have to pick one, recreating the drift D1 stops.
- **Operations inside the existing domain packages** (`store`, `shopping`, `household`). No new tree,
  but `store`, already the largest package, would take on orchestration and membership checks on top
  of persistence.

**Obligations:**

1. The discrete-API ADR records `internal/api/<resource>` and its relation to ADR-00012.

## D4 — MCP tools call the service layer, like web handlers

**Decided.** The MCP server is a caller of `internal/api/<resource>`, beside the web handlers. Its tools
stop calling `store` and the other domain packages directly, resource by resource as D5 migrates them.

**Why.** Web/MCP drift is the trigger for D1; a layer MCP does not call would not stop it.

**Obligations:**

1. Each migration node switches the MCP tools for its resource along with the web handlers.

## D5 — Migrate one resource at a time, each fenced by a lint rule

**Decided.** Each resource moves in its own plan node: the node creates `internal/api/<resource>`, moves
that resource's operations in, switches the web handlers and MCP tools to call it, and adds a
golangci-lint `depguard` rule that stops `internal/web/` and `internal/mcp/` from importing that
resource's domain packages directly again. When the last resource moves, the rules cover everything.
The pilot is shopping, chosen by the dispatcher because both the web and MCP call it, so the pilot sets
the pattern (service signature, caller type, error mapping, the depguard rule) on both doors. Recipes
follow. From the moment a resource's package exists, new operations on that resource go there; work
already decided (re-import, ADR-00042) is built in `api/recipes` if that has landed, and otherwise
moved by the recipes node.

**Why.** Each PR stays reviewable and each node's lane is one resource, so migration runs beside other
work, and the lint rule keeps a migrated resource migrated.

Rejected:
- **One sweep.** Rewrites most of `internal/web/` and `internal/mcp/` at once, colliding with every
  in-flight node and too large to review well.
- **Opportunistic** (new code through `api/`, old code when touched). Cheap, but it never finishes and
  drift continues in untouched code.

**Obligations:**

1. A pilot node migrates shopping and adds its depguard rule; the next node migrates recipes.
2. The remaining resources each get a node, and the last one leaves `internal/web/` and `internal/mcp/`
   with no direct import of `store`, `shopping`, `household` or `catalog` outside `internal/api/`.
