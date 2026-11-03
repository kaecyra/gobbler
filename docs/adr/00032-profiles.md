# ADR-00032: Everyone has a profile, and one visibility rule decides what a viewer sees

**Status:** Accepted

**Date:** 2026-11-03

## Context

A person is an email and nothing else (ADR-00018, ADR-00030). The owner wants a
profile page that holds:

- a display name and an avatar;
- the person's API keys (ADR-00031);
- their signed-in sessions, which they can see and end.

People see each other already: members of one household, and the sender and
receiver of a shared recipe. "Friends" will come later. What one person may see
of another must be able to depend on how they are related, even though today
the answer is "everything".

The header's light and dark button sits where an avatar would naturally go.

Options considered for visibility:

- **Show every field to every viewer, and add limits when friends arrive.**
  Rejected. Each page that shows a person would then need its own check
  added later. A missed one leaks.
- **A per-field privacy setting for each person now.** Rejected for now.
  Nobody has asked to hide a field, and the settings UI would be built for a
  need that does not exist yet.
- **One rule that every view of a person goes through, and that today
  allows everything.** Chosen.

For the default avatar, a remote generator such as vanillicon.com was
rejected. It would load content from a third party, and it would send that
party a hash of every member's email (AGENTS.md, Boundaries).

## Decision

### Profiles

- Every person who has signed in has a profile, whether or not they belong to
  a household. A non-member may open their own profile page, so admission adds
  it to what a non-member is admitted to.
- A profile is keyed by the email.
- A profile page has an opaque random address, never the email, so a profile
  address does not reveal or let anyone guess an email.
- The fields today are the email, a display name and an avatar. The name
  falls back to the email's local part.

### Visibility

- Every page or fragment that shows a person's profile fields to someone else
  asks one rule. The rule takes the viewer, the person and the field. It
  answers by their relation: **self**, **same household**, **friend** (reserved,
  not yet used) or **other signed-in person**.
- Today the rule shows every field to every relation that can reach a profile.
  Adding a limit later means changing the rule, not each page.
- Reaching a profile means being given its address: through a household member
  list, a shared recipe, or your own menu. There is no directory of people.
- The API-key and session sections are only ever shown to self.

### Avatar

- Upload, crop, processing and storage are set out in ADR-00033.
- Until a person uploads one, their avatar is generated in the app from a hash
  of their email: a simple, distinct pattern. Nothing is fetched from outside.

### Sessions on the profile

- A session also records the browser's user agent and the client IP address
  when it starts. Behind the Cloudflare tunnel, the IP comes from the
  `CF-Connecting-IP` header.
- The profile lists the person's sessions: the browser and operating system
  (read from the user agent), the IP, when it started and when it was last
  used. It marks the current session. The person may end any one session, or
  every session except the current one.
- The IP is shown for the person's own information only, and is never used to
  allow or refuse anything. Anything that reaches the app's port directly can
  set that header to any value (ADR-00027).

### Header

- The avatar takes the place of the light and dark button. Pressing it opens a
  menu with:
  - the light and dark toggle;
  - a link to the profile;
  - Sign out, which moves there from the "More sections" menu.
- A signed-out visitor, such as one on a public page (ADR-00029), sees a
  "signed-out" figure in the same place. It opens the same menu, offering the
  light and dark toggle and Sign in.
- The theme never follows the operating system's setting (`docs/ui/design.md`).

## Consequences

The `sessions` table of ADR-00030 gains a user agent and an IP address. These
are new personal data. They are kept only as long as the session row, which
ending or expiring the session deletes.

Membership and admission are unchanged (ADR-00018). Profiles add a page
that a non-member may reach. They do not let anyone see another household's
data.

**Obligations:**

1. Every view of another person's profile fields goes through the visibility
   rule. A test shows the rule is asked with the right relation for a viewer in
   the same household and for one who is not.
2. API keys and sessions appear only on your own profile. A test shows another
   member's view of a profile has neither.
3. A profile's address is random and does not contain the email. A test shows
   an unknown or guessed address gives 404.
4. A session row records the user agent and the IP address. Ending one session,
   and ending every session but the current one, are each tested.
5. The default avatar is generated in the app. A test shows the page loads
   nothing from another origin.
6. The header shows the avatar menu when signed in and the signed-out figure
   when signed out. The light and dark toggle works from both. An e2e test
   covers each.
7. `docs/ui/design.md` describes the avatar menu and the signed-out figure.
   `docs/operations.md` lists what a session records.
