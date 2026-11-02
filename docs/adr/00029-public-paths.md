# ADR-00029: A fixed set of public paths is served without an identity

**Status:** Accepted

**Date:** 2026-11-02

## Context

Every page of gobbler has needed a verified identity, except the health check
([ADR-00003](00003-cloudflare-access-identity.md), obligation 2: "the health
check is the only unauthenticated route"). Identity decides admission
([ADR-00018](00018-households-as-tenants.md)), and the app is the lock.

The owner wants some pages that anyone can read without signing in, and a
clear line between those and the private rest of the site. The first is
`/canada`. The footer's "in Canada" links to it, and it says where gobbler's
data lives and why. The not-a-member page is drawn with the embedded assets,
and a public page needs them too.

How identity is established is changing separately: Cloudflare Access today,
and sign-in inside the app later. The public/private line must not depend on
which of the two is in place.

Three options were considered:

- **Host public pages outside gobbler** and link to them from the footer. This
  was rejected. The owner wants the page to be part of the app, styled like
  it, and versioned with the code it describes.
- **Let whatever sits in front of the app decide what is public**, for example
  an Access Bypass rule, with the app serving any request that arrives without
  an identity. This was rejected. The line would then live outside this
  repository, and a rule that is too broad would open every page without the
  app noticing.
- **The app keeps its own fixed list of public paths.** This was chosen. The
  line is in one place in the code, and any edge configuration can only narrow
  what the app already allows, never widen it.

## Decision

The **public paths** are the health check, `/canada`, and the embedded assets
under `/static/`. They are one list in package `layout`, beside the admission
rule (`layout.Admits`). The identity middleware and the membership middleware
both read that list. A path is public only if it matches a page path exactly,
or lies under the assets prefix. The assets prefix is the only prefix match.

On a public path, the identity middleware establishes an identity if the
request carries valid proof of one. It serves the request without an identity
if the request carries none, or proof that fails. Every other path requires a
verified identity, as before.

The membership middleware admits a request with no identity only to a public
path, checked on both the decoded and the escaped form of the path. Every
identity, member or not, may open a public path.

A public page renders nothing from any household. Without an identity, the nav
is empty, because `layout.Admits` admits such a request to no section.

Adding a public path means adding it to the list and amending this ADR. A page
that reads household data is never public.

## Consequences

ADR-00003 obligation 2 is amended: the public paths, not the health check
alone, are served without authentication.

When something in front of the app also gates requests, such as Cloudflare
Access, it must let the public paths through, or they still ask for a sign-in.
That mismatch exposes nothing.

**Obligations:**

1. The public list is one declaration in `layout`, and both middlewares read
   it. A test checks that the health route and `/canada` are on it.
2. Against the real identity verifier (not the development bypass), a request
   with no identity gets each public path, and gets a 403 for a member page,
   an operator page, invitation acceptance and the not-a-member page.
3. A request with invalid proof of identity is served without an identity on a
   public path, and refused on any other path.
4. `docs/operations.md` no longer says that `/healthz` is the only route served
   without authentication.
5. ADR-00003 carries a note at obligation 2 pointing here.
