# ADR-00016: Cross-origin requests are refused by Fetch metadata, with a SameSite=Lax session cookie

**Status:** Accepted

**Date:** 2026-10-12

## Context

The sign-in session ([ADR-00030](00030-google-sign-in.md)) proves who the
browser belongs to. It does not prove that the request came from gobbler's own
pages: a page on another site can make the same signed-in browser send a
cross-site POST, and the session check passes it if the browser attaches the
session cookie. Whether it does is the cookie's SameSite attribute: `None`
sends the cookie on cross-site requests. The ingest form
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
- **SameSite on the session cookie alone.** One attribute: with `Lax`, the
  browser does not attach the cookie to a cross-site POST, so the request
  arrives without a session and is refused. But it is a single attribute on a
  single cookie, and a change to it silently removes all protection.
- **A synchronizer or double-submit token** (for example `gorilla/csrf`).
  Covers browsers that send no Fetch metadata. Costs a secret, a token in every
  form and an HTMX header on every request. That plumbing touches every page
  for a threat the two cheaper layers already cover for this one user's
  browsers.
- **The middleware plus SameSite=Lax.** Chosen. The middleware is the
  enforced, tested layer. The cookie attribute strips the session from the
  same requests, and covers what the middleware lets through.

## Decision

- `internal/web` wraps the whole router in `http.CrossOriginProtection` with no
  trusted origins and no bypass patterns. No handler opts out.
- State changes happen only on unsafe methods. A GET, HEAD or OPTIONS handler
  never changes state, because the middleware always allows those methods.
- The app sets its session and sign-in cookies SameSite **Lax** (ADR-00030).
  `Strict` is not used: the browser would then withhold the cookies on the
  redirect back from the issuer and on any link followed from another site.

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
2. The app sets the session cookie SameSite=Lax itself, so no operator step
   is needed. The e2e sign-in suite checks the attribute on the cookie the
   callback sets.
3. No route under `internal/web` changes state in response to GET, HEAD or
   OPTIONS.
