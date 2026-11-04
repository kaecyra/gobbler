# ADR-00036: The public OAuth endpoints are rate-limited per client address, and unused client registrations have a ceiling

**Status:** Accepted

**Date:** 2026-11-04

## Context

ADR-00034 gives each MCP credential its own leaky-bucket limit, but client
registration and the token endpoint are public (ADR-00029, as amended by
ADR-00034) and carry no credential to limit. Registration writes a row on
every call. Clients that never receive a grant are pruned after a day, and the
whole client table had a fixed ceiling, so a flood of registrations refused
every new connection, including real ones, until the pruning caught up.

The only per-request identity on those endpoints is the caller's address.
Behind the tunnel that is the `CF-Connecting-IP` header cloudflared sets, but
gobbler is published on every interface of the VM (ADR-00027), so anything on
the private network can reach it directly and send that header with any value.
The session list already reads the header unconditionally to show where a
session signed in from.

Options considered for limiting the public endpoints:

- **No limit; rely on the table's ceiling, with an edge rule documented.** No
  code. Rejected by the owner: a flood still blocks new connections.
- **A Cloudflare rate-limiting rule only.** Stops traffic before it arrives.
  Rejected: it lives outside the repository, unreviewed and free to drift.
- **A per-address leaky bucket in the app, trusting the forwarded address only
  from configured proxies.** Chosen. It reuses the limiter MCP has, and a
  caller that is not a trusted proxy is keyed by its own address, so it cannot
  choose its key.

Options considered for the client table's ceiling:

- **Remove it.** Rejected: a per-address limit slows a flood from one address,
  not one spread across many, and without a ceiling a day of registrations from
  every source, each up to the registration size limit, lands in the database
  and its backups.
- **Keep a ceiling on all clients.** Rejected: clients with grants count
  against it, and a flood of unused registrations blocks real ones.
- **A ceiling on clients without a grant only, set far above real use.**
  Chosen. A client with a grant is never pruned or counted, and the disk stays
  bounded.

## Decision

- Client registration and the token endpoint are each held to a leaky bucket
  per client address. A request past it gets `429` with `Retry-After`, as on
  `/mcp` (ADR-00034). The rate and burst are configuration, with defaults sized
  for one home server.
- The client address is the request's peer address, unless that peer is in a
  configured list of trusted proxies, in which case it is `CF-Connecting-IP`.
  With no trusted proxies configured, the header is never read.
- There is one limiter implementation and one client-address rule in the
  binary, shared by MCP, these endpoints and the session list.
- At most 10,000 clients without a grant are kept. A registration past that is
  refused with an error saying registrations are full; clients with a grant do
  not count towards it and are never pruned. Pruning of unused clients after a
  day is unchanged.

## Consequences

This amends ADR-00034's Limits: the public OAuth endpoints gain a limit per
client address beside the per-credential limit on `/mcp`.

The session list's "signed in from" address can no longer be spoofed from the
private network once trusted proxies are configured; before that, it shows the
peer address.

**Obligations:**

1. The leaky bucket used by `/mcp` is moved to a package both MCP and the
   OAuth endpoints import, not copied.
2. One client-address function replaces the session package's unconditional
   read of `CF-Connecting-IP`. Tests show the header is used from a trusted
   peer and ignored from any other, and ignored entirely with no trusted
   proxies configured.
3. Tests show registration and the token endpoint each answer `429` with
   `Retry-After` past the burst for one address, while another address is
   unaffected.
4. The client ceiling counts only clients without a grant and is 10,000. A test
   fills it, shows the next registration is refused, and shows a client with a
   grant is neither counted nor pruned.
5. `docs/operations.md` describes the trusted-proxy setting (with the
   cloudflared Compose network as the example) and the rate and burst
   settings for the public OAuth endpoints.
