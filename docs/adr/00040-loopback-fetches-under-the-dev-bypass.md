# ADR-00040: The fetch guard admits loopback only while the dev auth bypass is on

**Status:** Accepted

**Date:** 2026-11-05

## Context

ADR-00039 "Fetching" puts every fetch made on someone's behalf behind one
address guard, which refuses any address that is not public unicast. Loopback
is one of the refused ranges.

The end-to-end suite (ADR-00013) imports recipes by URL from a fixture page it
serves on `127.0.0.1`. Once the page fetch moved onto the guard, every one of
those imports was refused, as ADR-00039 says it must be. Nothing in the
existing record says how a test may reach a page on the machine it runs on.
The OAuth metadata fetch uses the same guard, but its end-to-end tests use
dynamic registration and never fetch a document from loopback, so there was no
precedent to follow.

The dev auth bypass (ADR-00003, kept as it was by ADR-00030) is already a
development-only mode. Its config setting refuses to start the server unless it
listens on a loopback address, Compose forces it off (ADR-00027), and the
end-to-end server runs with it on.

Options considered:

- **Admit loopback while the dev auth bypass is on.** The guard still refuses
  every other private, link-local and non-unicast range. The end-to-end import
  specs keep fetching a real page over HTTP, and a local dev server can import
  a page served on the same machine. Cost: one setting widens the guard, and a
  mistake in that setting's own check would widen it in production too.
  Chosen.
- **No switch: the end-to-end specs stop importing by URL from a live
  server.** They would paste the page text, or go through a test-only fetcher
  in the end-to-end fixture binary, and the guard would have no off switch.
  Rejected: fetching a page by URL would then be covered only by Go unit tests
  with an injected dialer, and that path is the one a person uses most.
- **A setting of its own, separate from the dev auth bypass.** Rejected: it
  would be a second development-only switch whose loopback-only start check
  repeats the bypass's, and an operator could set it in production on its own.

## Decision

- While the dev auth bypass is on, the guarded client used for the recipe page
  fetch and the image candidate fetch admits loopback addresses: `127.0.0.0/8`
  and `::1`. Nothing else changes for them: every other private, link-local or
  non-unicast address is still refused, proxies are still ignored, and the
  redirect limit and scheme rule of ADR-00039 still apply.
- The OAuth metadata fetch keeps refusing loopback in every mode. Its tests do
  not need it, and a client metadata document on loopback is not a case anyone
  has.
- The admission comes from the dev auth bypass setting, through the ingestion
  configuration. The guard does not read the environment itself.

## Consequences

ADR-00039 "Fetching" is amended: its "not public unicast" rule has one
exception, for loopback under the dev auth bypass. Production behaviour is
unchanged. The bypass's existing loopback-listen check is now also what keeps
this exception out of production.

**Obligations:**

1. ADR-00039 "Fetching" carries an "Amended by ADR-00040" note at the bullet
   that refuses addresses that are not public unicast.
2. A Go test shows that a guarded page fetch to a loopback address is refused
   when the dev auth bypass is off and allowed when it is on, and that a
   private non-loopback address (for example `10.0.0.1` or `192.168.1.1`) is
   refused in both modes.
3. A Go test shows that the OAuth metadata fetch refuses a loopback address
   with the dev auth bypass on.
4. The end-to-end import specs that fetch the fixture page by URL pass with
   the guard in place, unchanged.
5. `docs/operations.md`, where it says outbound fetches never reach private
   addresses, adds that a development server running with the dev auth bypass
   may fetch from loopback.
