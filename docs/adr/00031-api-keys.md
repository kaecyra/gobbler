# ADR-00031: Personal and household API keys authenticate the machine-facing surface

**Status:** Accepted

**Date:** 2026-11-03

## Context

The owner wants agentic conversations and processes to drive recipes and meal
plans over MCP. A browser session (ADR-00030) is the only identity today, and
an agent has no browser. It needs a credential it can hold and send.

Some automations act for one person. Others act for the kitchen as a whole and
should not stop working when the person who set them up is away. Those are two
kinds of key.

Options considered:

- **No keys, so agents hold a session cookie.** Rejected. A session belongs to
  a signed-in browser and lasts 30 days from sign-in. An agent would need
  someone to sign in again every month and paste a cookie, and the cookie
  would open every HTML page, including membership changes.
- **Personal keys only.** Rejected. A household-wide automation would then
  belong to whoever made it, and would break when that person leaves.
- **Personal and household keys.** Chosen.

## Decision

### Kinds of key

- A **personal key** belongs to one person (their email, ADR-00018) and acts as
  that person. Any signed-in person may create one. It is managed on their
  profile page (ADR-00032).
- A **household key** belongs to one household. It acts for the household
  rather than as a member: it reads and writes that household's recipes,
  shopping lists and meal plans. It is managed on the household page, with the
  household's other settings. Members are equal (ADR-00018), so any member may
  create or revoke one.

### What a key reaches

- A key authenticates only the machine-facing surface: the MCP endpoint, and
  any API that is added later. The HTML pages ignore keys. Their identity is
  the session (ADR-00030), so a leaked key cannot drive the browser UI.
- No key can create or revoke keys, end sessions, change household membership
  or issue invitations. No key reaches an operator page (ADR-00018). These stay
  with a signed-in person.
- Admission is the same membership check as for a session (ADR-00018):
  - A personal key reaches the household its owner belongs to, and nothing
    when its owner belongs to none.
  - A household key reaches its own household only.
- Every write made with a key records which key made it.

### Secrets, expiry and revocation

- A key is a random secret shown once, when it is created. Only its hash is
  stored, as with sessions (ADR-00030). A short non-secret prefix and a name
  identify it in the list.
- Each key has its own expiry, chosen when it is created, and "no expiry" is
  one of the choices. An expired or revoked key is refused.
- Each key records when it was last used.
- Revoking a key deletes it, and the next request that sends it is refused.
- In the same change as removing a member or their leaving (ADR-00030 already
  ends their sessions there):
  - their personal keys are revoked;
  - every household key they created in that household is revoked, because
    they have seen its secret.
- Deleting a household revokes its household keys.
- No log line contains a key.

## Consequences

ADR-00030 names the session cookie as the identity. That stays true for every
HTML page. This record adds a second credential, accepted only on the
machine-facing surface. A key arrives in the `Authorization` header, never in
a cookie, so the cross-origin protection of ADR-00016 is not needed for it and
is not weakened.

The MCP endpoint itself, its tools and its transport are a separate decision.
This record settles the credential and what it may reach. Until that endpoint
exists, keys can be managed but no route accepts one.

**Obligations:**

1. Keys are stored as hashes, never in plain text. A test shows that the secret
   appears only in the response that created it.
2. A test covers each refusal: expired, revoked, unknown, a key sent to an HTML
   page, and a key sent to a key-management, session, membership, invitation or
   operator route.
3. Removing a member, or a member leaving, revokes their personal keys and the
   household keys they created, in the same transaction that removes them.
   Deleting a household revokes its household keys. Each case is tested.
4. A personal key's admission and a session's admission for the same person
   are decided by the same membership check. A test shows a key from another
   household is refused.
5. Writes made with a key record the key's id.
6. No log line contains a key. Code review checks this, as it does for
   ADR-00030's tokens.
7. `docs/operations.md` describes both kinds of key, where each is managed, and
   their expiry choices.
