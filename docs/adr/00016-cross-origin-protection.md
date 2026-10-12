# ADR-00016: Cross-origin requests are refused by Fetch metadata, with SameSite=Lax at the edge

**Status:** Accepted

**Date:** 2026-10-12

## Context

Cloudflare Access proves who the browser belongs to (ADR-00003). It does not
prove that the request came from gobbler's own pages: a page on another site
can make the same logged-in browser send a cross-site POST, and Access passes
it, because the browser attaches the Access cookie. The `CF_Authorization`
cookie's SameSite attribute is an Access application setting, and its default
is `None`, which sends the cookie on cross-site requests. The ingest form
already accepts POSTs, and the editor, history, journal, catalog and settings
pages will add more.

gobbler is single-user, served over one hostname, with HTMX making same-origin
requests. Go 1.25 added `net/http.CrossOriginProtection`, which rejects unsafe
methods (anything but GET, HEAD and OPTIONS) when the browser's
`Sec-Fetch-Site` header marks the request cross-origin, falling back to
comparing `Origin` with `Host` when that header is absent. Every major browser
has sent `Sec-Fetch-Site` since 2023.

Options considered:

- **`http.CrossOriginProtection` alone.** One standard-library middleware, no
  token, no state, nothing in templates or HTMX calls, and testable in the
  repository. A browser that sends neither `Sec-Fetch-Site` nor `Origin` is let
  through.
- **SameSite on the Access cookie alone.** No code: with `Lax`, the browser
  does not attach the cookie to a cross-site POST, so Access refuses the request
  at the edge. But it is a dashboard setting. Nothing in the repository enforces
  or tests it, and changing it back silently removes all protection.
- **A synchronizer or double-submit token** (for example `gorilla/csrf`).
  Covers browsers that send no Fetch metadata. Costs a secret, a token in every
  form and an HTMX header on every request. That plumbing touches every page
  for a threat the two cheaper layers already cover for this one user's
  browsers.
- **The middleware plus SameSite=Lax.** Chosen. The middleware is the
  enforced, tested layer. The cookie setting stops the same requests at the
  edge before they reach the tunnel, and covers what the middleware lets
  through.

## Decision

- `internal/web` wraps the whole router in `http.CrossOriginProtection` with no
  trusted origins and no bypass patterns. No handler opts out.
- State changes happen only on unsafe methods. A GET, HEAD or OPTIONS handler
  never changes state, because the middleware always allows those methods.
- The Access application's cookie setting is SameSite **Lax**. `Strict` is not
  used: Cloudflare documents that it can cause redirect loops during login.

## Consequences

Routes added later inherit the protection without doing anything. No form or
HTMX request carries a token. Any cross-origin integration that POSTs to
gobbler, such as a future import endpoint called from another site, needs a
new ADR before it can be allowed.

**Obligations:**

1. `internal/web` applies `http.CrossOriginProtection` to the router built by
   `web.New`. A test proves that a POST with `Sec-Fetch-Site: cross-site` is
   refused with 403, and that a same-origin POST and a cross-site GET are
   allowed.
2. `docs/operations.md`'s Cloudflare section tells the operator to set the
   Access application's SameSite cookie setting to Lax, and says why.
3. No route under `internal/web` changes state in response to GET, HEAD or
   OPTIONS.
