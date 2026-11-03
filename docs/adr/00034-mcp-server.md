# ADR-00034: gobbler serves MCP, authenticated by API keys or OAuth, with recipe writes held for review

**Status:** Accepted

**Date:** 2026-11-03

## Context

ADR-00031 created API keys for a machine-facing surface that did not exist yet.
The owner wants agents to drive recipes, shopping lists and, later, meal plans
over the Model Context Protocol.

The current MCP specification (revision `2026-07-28`) makes authorization
optional. A server that supports it over HTTP SHOULD act as an OAuth 2.1
resource server:

- it publishes Protected Resource Metadata (RFC 9728);
- it accepts only access tokens issued for it.

Some clients let a person set a header, and a static key works with them.
Others, including some hosted connector screens, can only run the OAuth flow.

ADR-00006 holds LLM output back until a person has reviewed it. An agent is an
LLM. Recipes carry notes and history that cannot be recreated (AGENTS.md).
A shopping list is cheap to correct.

Options considered for authentication:

- **Keys only, with OAuth later.** Rejected by the owner, who wants every MCP
  client to work from the start.
- **OAuth only.** Rejected. A household-wide automation would need a person's
  grant and would stop when that person leaves, which is what household keys
  exist to avoid (ADR-00031).
- **Keys and OAuth.** Chosen.

Options considered for agent writes:

- **Every write held for review.** Rejected. An agent could not finish a
  shopping run on its own.
- **Every write applied directly.** Rejected. A wrong recipe edit corrupts data
  that cannot be recreated, and recipe history would be the only undo.
- **Recipe writes held for review, list writes applied.** Chosen.

Options considered for key permissions:

- **Read-only and read-write keys.** Rejected, with per-tool scopes, as
  unneeded for now. Expiry and revocation (ADR-00031) are the controls.

## Decision

### Endpoint

- The gobbler binary serves MCP over Streamable HTTP at `/mcp`. There is no
  stdio server.
- Admission to `/mcp` is the membership check of ADR-00018, applied to whoever
  the credential belongs to.
- HTML pages ignore both kinds of MCP credential (ADR-00031).

### Credentials

`/mcp` accepts either kind of credential, as `Authorization: Bearer`:

- An **API key** (ADR-00031), personal or household.
- An **OAuth access token**, issued by gobbler itself. gobbler acts as an
  OAuth 2.1 authorization server:
  - **Discovery:** it publishes the Protected Resource Metadata and the
    Authorization Server Metadata the spec requires.
  - **Client registration:** it accepts Client ID Metadata Documents and
    Dynamic Client Registration. Fetching a client's metadata document is an
    outbound request controlled by an outsider, so it must be HTTPS, must not
    resolve to a private or loopback address, and is bounded in size and time.
  - **Authorization:** a person signs in with their session (ADR-00030), then
    approves the client on a consent page. The page names the client and the
    host it will return to.
  - **Exchange:** PKCE and the `resource` parameter are required, and each
    token is bound to gobbler's `/mcp` as its audience.
  - **Tokens:** access tokens are short-lived. Refresh tokens rotate, and
    reusing a rotated refresh token revokes the whole grant. Only hashes are
    stored, as for sessions and keys.
- An OAuth grant belongs to one person and acts as that person, with the same
  reach as a personal key. There are no household OAuth grants.
- A person's grants are listed on their profile page, next to their keys
  (ADR-00032), and each can be revoked. Removing a member, or a member leaving,
  revokes their grants in the same change that revokes their keys (ADR-00031).
- A credential that is missing, unknown, expired or revoked gets `401` with a
  `WWW-Authenticate: Bearer` header that points at the resource metadata.

### Permissions

A credential reaches everything its holder could reach through MCP. It has no
read-only or per-tool scope. ADR-00031's exclusions still hold: no MCP tool
manages keys, grants, sessions, membership, invitations or operator pages.

### Writes

- An MCP tool that creates or changes a recipe produces a draft. The draft
  waits on the existing review screen for a person to confirm (ADR-00006).
- Shopping-list and pantry changes apply directly.
- Every write records the credential that made it (ADR-00031, obligation 5).

### Limits

- Each credential has its own leaky-bucket rate limit. A request past it gets
  `429` with `Retry-After`. The rate and the burst are configuration, with
  defaults sized for one home server.
- A tool that returns a list returns one page at a time. A page has a maximum
  size, and the response carries a cursor for the next page.

## Consequences

This amends ADR-00031. An OAuth grant is a second machine credential beside
the API key, under the same exclusions and revocation rules.

This amends ADR-00006. MCP writes to recipes join ingestion as a source of
drafts that wait for review. MCP writes to shopping lists and the pantry are
not held. The AGENTS.md boundary is narrowed to say so.

This amends ADR-00029. The discovery documents, client registration and the
token endpoint must be reachable without a session, so they join the public
paths. The authorization endpoint needs a session and is not public.

Meal-plan tools wait for meal planning itself (#38).

**Obligations:**

1. `/mcp` speaks Streamable HTTP with the official Go SDK. Its version comes
   from the module proxy when it is added.
2. A test covers each refusal, for both an API key and an OAuth token:
   - missing, unknown, expired and revoked credentials;
   - a credential from another household;
   - a credential sent to an HTML page.
3. The OAuth flow is tested end to end against a test client. Separate tests
   cover each refusal:
   - a missing or wrong PKCE verifier;
   - a missing or wrong `resource`;
   - a token whose audience is another server;
   - a reused refresh token, which also revokes the grant;
   - a client metadata URL that is not HTTPS or resolves to a private address.
4. Removing a member, or a member leaving, revokes their OAuth grants in the
   same transaction as their keys. Tested.
5. A recipe write through MCP creates a draft and changes no recipe until a
   person confirms it. A shopping-list write applies at once. Each is tested,
   and each records the credential's id.
6. A burst past the limit gets `429` with `Retry-After`. A list tool returns at
   most one page, with a cursor. Each is tested.
7. AGENTS.md's Boundaries line about LLM output names recipes and this ADR.
8. `docs/operations.md` describes:
   - how to connect an MCP client with a key, with an example client
     configuration;
   - how to connect one over OAuth;
   - where grants are revoked;
   - the rate-limit settings.

   `docs/design.md` adds the MCP and OAuth packages to its package table.
