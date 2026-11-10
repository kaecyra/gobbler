# ADR-00055: Invitations are emailed over SMTP with TLS, as HTML with a plain-text part

**Status:** Accepted

**Date:** 2026-11-10

## Context

An invitation is addressed to one email ([ADR-00018](00018-households-as-tenants.md),
[ADR-00028](00028-invitation-codes.md)), but gobbler never sends it: the
inviter copies the link from the household or operator page and passes it on.
Issue #615 asks gobbler to email the link when the invitation is issued.

Options considered for the transport:

- **A provider's HTTP API (Postmark, Resend, SES).** Goes out over HTTPS, so
  no outbound mail port has to be open, and returns structured errors and
  delivery reports. Rejected: it ties the code to one vendor and its SDK, and
  the delivery reports would feed nothing, since gobbler has no place that
  shows them.
- **SMTP submission.** Chosen. Every transactional provider and every mail
  server accepts it, so changing provider is configuration, the same
  property [ADR-00007](00007-llm-provider-interface.md) kept for LLM providers.
  The standard library speaks it, so it adds no dependency. The owner's
  network does not block outbound submission ports.

Options considered for the message body:

- **Plain text only.** Makes open tracking impossible, since a tracking pixel
  needs HTML. Rejected by the owner, who wants HTML mail.
- **HTML with a plain-text alternative.** Chosen.

## Decision

### Transport

- Mail is sent by SMTP submission to a host, port, user and password set on
  the operator's Deployment page, the password held as an encrypted secret
  ([ADR-00057](00057-integration-settings-in-the-database.md)).
- **Every connection is encrypted before anything else is sent.** Either
  implicit TLS from the first byte, or STARTTLS that the client insists on.
  The port decides which: port 465 is implicit TLS, and any other port
  requires STARTTLS. There is no separate setting for the mode.
  A server that does not offer STARTTLS gets no message and no credentials:
  the send fails. The server's certificate is verified against the configured
  host. There is no setting that turns encryption or verification off.

### Body

- Each message is `multipart/alternative`: a plain-text part and an HTML part
  carrying the same content.
- The HTML loads nothing remotely: no images, fonts or stylesheets by URL.
  Styles are inline.

### Tracking

Mail providers can add open tracking (an image in the HTML) and click
tracking (links rewritten through the provider), often by default. Both are
third-party trackers, which AGENTS.md rules out. gobbler cannot see what the
provider does after the hand-off, so the rule is held by the provider's own
settings: open and click tracking are turned off there before mail is
configured, as a written deployment step.

Two stronger options were rejected. Per-vendor headers that switch tracking
off per message cover only the vendors gobbler knows, and bring back the
vendor coupling SMTP was chosen to avoid. A test-send button for a person to
inspect the delivered message is a check by eye, not by the binary, and is
not added for this purpose.

### Configuration

Email is optional. With no SMTP host configured, invitations behave as before
this ADR: created, and shared by copying the link. Setting the host makes the
port, user, password and from address required: the Deployment page refuses
to save a partial mail configuration and says which field is missing.

### Sending, failure and resending

The email is sent during the request that creates the invitation, under a
timeout. A send that fails never undoes the invitation: it is created, the
failure is logged, and the failure is stored.

Each invitation records when its email last went out and the last send
error. Every pending invitation shows that status beside its copy link, on
the household page and the operator page alike. Every pending invitation has
a **Resend** button, whether its last send failed, succeeded, or never
happened because mail was not configured when it was created. Inviting the
same address again is not a resend: it creates a second, separate
invitation.

Two options were rejected. Reporting the result only on the redirect after
the invite, with nothing stored, loses it on reload and leaves no record of
whether a pending invitation was ever emailed. An outbox table with a
background worker and automatic retries recovers from brief outages without
a person, but it is a table, a worker and a backoff policy for something a
household does a few times a year, and permanent failures still need showing.

### Rate

Anyone who can issue an invitation today can cause a send: a household member
inviting into their household, and the operator inviting a new household.
Every send goes to an outside address through the owner's provider account,
so a stolen session or a looping bug could cost that account or the sending
domain its standing, and fill a stranger's inbox.

Sends are held to a leaky bucket per sender, keyed by the verified email of
the person sending, using the one limiter implementation
[ADR-00036](00036-rate-limits-on-public-oauth-endpoints.md) requires. Invites
and Resends draw from the same bucket. The rate and burst are configuration,
with defaults sized for one household. A send past the limit is not
attempted: an invite still creates the invitation, and the refusal is stored
and shown as its send error, as any other failed send is.

Two options were rejected. No limit leaves nothing to bound the stolen-session
or looping case short of the provider's own limits, which act after the
damage. A cap per invitation protects one recipient from repeated Resends but
not many recipients from one sender, and the per-sender bucket already bounds
what any one recipient can receive.

### Content

The email says who sent the invitation and into which household, or, for an
operator's invitation, that it is to start a household. It carries the link
and when the invitation expires. The HTML uses layout, type and gobbler's
colours for a clear link button, and carries **no image of any kind**.

Two options were rejected. An image attached to the message and referenced
inline (`cid:`) loads nothing remotely, but some mail clients show it as an
attachment or out of place, and the button and colours carry the look
without it. An image loaded from gobbler's own public address would tell the
server when each message is opened, which is the open tracking ruled out
above, run by gobbler itself.

## Consequences

**Obligations:**

1. A test runs the sender against an SMTP server that does not advertise
   STARTTLS and shows the send fails before any `AUTH` or `MAIL FROM`.
2. A test shows a server certificate that does not match the configured host
   fails the send.
3. A test parses a sent message and shows a `multipart/alternative` with a
   `text/plain` and a `text/html` part, no `<img`, `src=` or `url(` in the
   HTML, and no part other than those two.
4. `docs/operations.md` gains a mail section that names the SMTP settings on
   the Deployment page and, before them, a step to turn off open and click
   tracking at the provider.
5. `docs/design.md`'s configuration table gains a mail row saying the SMTP
   settings live on the Deployment page.
6. A test shows the Deployment page refuses a host without the rest, and that
   with no host saved an invitation is created and no send is attempted.
7. The `invitations` table gains a nullable time the email last went out and
   a nullable last send error, by migration.
8. Tests show a failed send leaves the invitation created with its error
   stored and shown, and that Resend on a pending invitation sends again and
   updates both fields without creating a second invitation.
9. Tests show that past the burst one sender's invite still creates the
   invitation with the refusal stored and no SMTP connection made, and that
   another sender is unaffected. The limiter is the shared one, not a copy.
10. The mail rate and burst are environment settings, as for MCP and OAuth.
    They join the mail row in `docs/design.md`'s configuration table,
    `.env.example` and the mail section of `docs/operations.md`.
