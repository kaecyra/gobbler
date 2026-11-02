# ADR-00028: An invitation is addressed by a random eight-character base62 code

**Status:** Accepted

**Date:** 2026-11-02

## Context

[ADR-00018](00018-households-as-tenants.md) (Invitations) says what an
invitation is: addressed to one email, accepted only by a request verified as
that email, single-use, expiring and revocable. It does not say how an
invitation is named in a link, and the first implementation used the
`invitations` table's integer primary key. Those keys count up from one, so
every link is guessable and reveals how many invitations the deployment has
issued.

The email binding is the lock and stays the lock. The identifier only has to
stop a link from being guessable or from counting anything.

Four options were considered:

- **Keep the integer key.** This was rejected for the reasons above.
- **Make a random code the primary key.** This was rejected. The integer key is
  never shown once a code exists, and changing it would touch every query that
  joins on it for no gain.
- **A long URL-safe token (22 or more characters), as for a password reset.**
  This was rejected. That length is for when the token is the only secret, and
  here it is not.
- **A short random code in its own column.** This was chosen.

Whether a person could type the code into the app, as well as open it as a
link, was also considered. This was rejected: the owner wants links only. That
leaves no reason to drop look-alike characters, so the alphabet is plain
base62.

## Decision

Every invitation has a **code**: eight characters drawn uniformly from the 62
characters `0-9`, `A-Z` and `a-z`, from a cryptographically secure source,
using rejection sampling so that no character is more likely than another.
Eight characters give about 47 bits.

The code is unique across all invitations, and the schema enforces this. A new
code that collides with an existing one is redrawn rather than reported as an
error.

The code is the **only** identifier of an invitation outside the database.
Each link and form action that names an invitation, whether to accept it or
revoke it, names it by code. The integer key stays the table's internal key
and never appears in a URL or on a page.

Codes are compared exactly, including case. A code that does not exist gets
the same response as one addressed to a different email, so the accept route
cannot be used to find out which codes exist.

## Consequences

Old invitation links that use the integer key stop working. Invitations
pending at upgrade time are given codes by the migration, and the inviter can
copy the new link from the household or operator page.

**Obligations:**

1. The `invitations` table gains a `code` column that is `NOT NULL` and
   `UNIQUE`, and a migration fills it for existing rows.
2. The household store issues the code when it creates an invitation, and
   looks up an invitation, accepts it and revokes it by code.
3. The accept route under `/invitations/` and the revoke routes on the
   household and operator pages take a code and reject anything else.
4. A unit test checks that generated codes are eight characters long and use
   only the base62 alphabet, and an e2e test accepts an invitation through its
   code link.
