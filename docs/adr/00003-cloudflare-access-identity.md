# ADR-00003: Identity from verified Cloudflare Access JWT; single user

**Status:** Accepted

**Date:** 2026-10-06

## Context

gobbler is reached only through a Cloudflare tunnel (`cloudflared`) with a
Cloudflare Access application in front of it. Access authenticates the person
before any request reaches the VM and forwards a signed JWT in the
`Cf-Access-Jwt-Assertion` header.

For now there is one user. Invitations for other household members may come
later (ADR-00011).

Options considered:

- **Build in-app login (passwords or OAuth).** Duplicates what Access already
  does, and adds password storage, reset flows and session security to a
  personal app. Rejected.
- **Trust the network: assume anything reaching the app came through Access.**
  Any misconfiguration — a published port, a second tunnel route, a container
  on the same network — becomes full access with nothing to stop it. Rejected.
- **Verify the Access JWT in the app.** Cheap, standard, and makes the app safe
  even if the network assumption breaks. Chosen.

## Decision

Every request except the health check passes through middleware that:

1. Reads `Cf-Access-Jwt-Assertion` (falling back to the `CF_Authorization`
   cookie).
2. Verifies the signature against the team's public keys at
   `https://<team>.cloudflareaccess.com/cdn-cgi/access/certs`, cached and
   refreshed on unknown `kid`.
3. Checks `iss` is the team domain, `aud` contains the configured application
   AUD tag, and `exp`/`nbf` are valid.
4. Puts the verified email in the request context as the user identity.

A request that fails any step gets `403` and is logged without the token.

The team domain and AUD tag are configuration. A development mode that bypasses
verification exists only behind an explicit flag and refuses to start if the
listen address is not loopback.

The app stores no passwords and has no login page. Data is not partitioned by
user yet; the identity is recorded on writes (notes, revisions, "made it" log)
so partitioning can be added when invitations land.

## Consequences

A Cloudflare outage makes the app unreachable, which is acceptable for a
personal tool. Moving off Cloudflare means replacing this middleware.

**Obligations:**

1. Requests with a missing, unsigned, expired, or wrong-`aud` token receive
   `403`, each covered by a test.
2. The health check is the only unauthenticated route.
3. The development bypass refuses to start on a non-loopback listen address.
4. No log line contains the JWT or the cookie value.
