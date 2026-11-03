# ADR-00030: The app signs people in over OpenID Connect, with Google; Cloudflare Access is removed

**Status:** Amended by [ADR-00031](00031-api-keys.md), [ADR-00032](00032-profiles.md)

**Date:** 2026-11-02

## Context

[ADR-00003](00003-cloudflare-access-identity.md) put Cloudflare Access in front
of the app, had the app verify the Access JWT on every request, and rejected
in-app login because it "duplicates what Access already does".
[ADR-00018](00018-households-as-tenants.md) then had Access admit any email
that authenticates, which made the app's membership check the only real lock.
At that point Access stopped being a barrier. Anyone with a Google account, or
any email address at all through the one-time PIN, gets past it.

What Access still provides is identity, plus a stop at Cloudflare's edge for
anonymous traffic to private paths. What it costs:

- A second place where the line between public and private pages has to be
  kept in step with the app. [ADR-00029](00029-public-paths.md) can be defeated
  by an Access rule that does not bypass the public pages.
- A dashboard configuration outside this repository.
- One more US service in the path of every request.

The owner wants Google sign-in and a single, clear line between the public and
private parts of the site.

Five options were considered:

- **Keep Access, with Google as its only login method.** This is still Google
  sign-in, done by Cloudflare. It was rejected because the line between public
  and private would still live in two places.
- **Passwords.** This was rejected. They bring storage, resets and breach
  handling, which AGENTS.md rules out.
- **A hosted identity broker, such as Auth0, to offer several providers.**
  This was rejected. It is a US company that would see every sign-in and keep
  a profile of each person, which would undo the reason for removing Access.
  If more providers are wanted later, a broker can be self-hosted alongside
  the VM (Dex, Authentik or Zitadel, for example). The app would talk to it as
  one more OpenID Connect issuer, with no change to its code.
- **Email magic links.** This was rejected. It needs outbound mail, which
  gobbler does not send.
- **A provider-neutral OpenID Connect client in the app, configured with
  Google.** This was chosen.

There were two options for sessions:

- **A stateless signed cookie, or a JWT.** This was rejected. It cannot be
  revoked before it expires, and signing-key rotation becomes another secret
  to manage.
- **A server-side session row in SQLite, named by an opaque random token in
  the cookie.** This was chosen. Logging out, or removing a member, ends the
  session at once.

## Decision

### Sign-in

The app is an OpenID Connect client of **one configured issuer**. It learns the
issuer's endpoints and signing keys from the issuer's discovery document, and
nothing in the code names a particular provider. Today the issuer is Google
(`https://accounts.google.com`). Moving to another provider, or to a
self-hosted broker, changes configuration and nothing else.

It uses the authorization-code flow with PKCE, a `state` value and a `nonce`.
It verifies the ID token's signature against the issuer's published keys,
along with its issuer, audience, expiry and nonce, and it accepts only
`email_verified: true`. The verified, lower-cased email is the identity,
exactly as the Access email was, so admission
([ADR-00018](00018-households-as-tenants.md)) does not change. The app stores
no passwords and no Google tokens; it keeps only the email.

The issuer URL, client ID and client secret come from the environment, as does
the public base URL that the redirect URI is built from. None of them is ever
committed or logged.

### Sessions

A successful sign-in creates a session row in SQLite. The row records a hash of
a random token, the email, and the times it was created and last used. The
cookie carries only the token. It is `HttpOnly`, `Secure`, `SameSite=Lax`, and
named with the `__Host-` prefix. A session lasts a fixed time from sign-in,
**30 days** unless the owner sets otherwise, and then ends. Signing out
deletes the row.

### Public and private

Sign-in and the OAuth callback join the public paths (ADR-00029). A request for
a private page that carries no valid session is handled as follows:

- A full-page `GET` is redirected to sign-in, and returns to the page it asked
  for afterwards. That return target must be a path on this site, so the
  redirect cannot send anyone elsewhere.
- Any other request gets `401`.

### Access is removed

Cloudflare Access is removed from the deployment and from the code. The
Cloudflare tunnel still carries traffic. Getting traffic in without Cloudflare
is a separate question. The development bypass (ADR-00003) stays as it is,
loopback only.

## Consequences

This supersedes ADR-00003. Its concern about trusting the network does not
return. The app verifies its own session on every private request, so a
published port or a second route still meets the lock.

Every path is now reachable from the internet without signing in first. The app
answers anything that is not public with a redirect or a 401, and the
membership check (ADR-00018) stays the lock on household data.

The email is the identity. A person who reached gobbler through Access with a
non-Google email (the one-time PIN) must be invited again under their Google
address.

**Obligations:**

1. Sign-in verifies `state`, PKCE, `nonce`, the signature, issuer, audience,
   expiry and `email_verified`. A test covers the failure of each check.
2. Sessions are stored server-side as token hashes. Signing out deletes the
   row, and an expired or deleted session is refused. Each case is tested.
3. The return target after sign-in is accepted only when it is a path on this
   site, and an absolute URL is refused. Tested.
4. No log line contains a session token, an authorization code or an ID token.
5. The Access middleware, its configuration and the Access steps in
   `docs/operations.md` are removed, and `docs/operations.md` describes the
   Google OAuth client setup and the new environment variables.
6. No source file names a provider. The issuer is configuration, and a test
   signs in against a fake issuer served by the test itself.
7. AGENTS.md's Boundaries line about Cloudflare Access identity is rewritten to
   name Google sign-in and this ADR.
8. Library versions come from the module proxy at implementation time, never
   from memory.
9. In the change that removes Access, ADR-00003's status line reads
   "Superseded by ADR-00030", and so does its row in the ADR index.
