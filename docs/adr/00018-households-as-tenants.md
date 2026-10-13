# ADR-00018: Households are tenants; a household has several members

**Status:** Accepted

**Date:** 2026-10-12

## Context

gobbler was built as a one-person recipe box. ADR-00003 has Cloudflare Access
decide who reaches the app, and the app records the verified email on recipe
writes (recipes, notes, the made-it log, revisions, source snapshots). It does
not record it on shopping data: lists, entries, manual items, the pantry and
check state carry no identity, and the pantry is one global set. The app keeps
no user or membership records of its own.

ADR-00011 deferred "invitations and multiple users" on the premise that the
recorded identity would make it a feature rather than a data migration. That
holds for recipe data and not for shopping data.

Three shapes were considered:

- **One shared household.** Everyone Access admits sees and edits everything,
  and inviting someone is an Access policy change. This is nearly free and
  matches one kitchen. It was rejected because the owner wants separate
  households on one deployment, and that model has no boundary between them.
- **Per-person partitioning.** Each email owns its own recipes, lists and
  pantry, and sharing is explicit. This was rejected because a kitchen is
  shared: a household's members cook from, shop for and check off one set of
  data, and per-person ownership would make every shared action an explicit
  share.
- **Households as tenants.** Data belongs to a household, and a household has
  several members. This was chosen.

Admission had three options:

- **Access as a strict allowlist kept in infrastructure code, with the app
  mapping emails to households.** This gives two locks, so a bug in the app's
  membership check exposes nothing to people Access has not admitted. It was
  rejected because every invitation would need an infrastructure deploy as
  well as an in-app step, and the two lists drift apart.
- **Access admits an email domain or group.** This was rejected because the
  people a household invites rarely share a domain or group the owner
  controls.
- **Access authenticates any email, and the app decides everything.** This was
  chosen, because inviting someone should be an act inside the app. The cost
  is that the app's membership check becomes the only lock.

There were three options for who creates households:

- **Open sign-up.** This was rejected. Once Access admits anyone, a stranger's
  household would spend the operator's disk, backup storage and LLM API budget
  (ADR-00007), and containing that would take quotas this ADR does not design.
- **The operator alone, from the command line.** This was rejected in favour
  of doing it in the app, so growing the deployment never needs a shell.
- **Invitation only**, issued by the operator or by members. This was chosen.

Membership per person had two options:

- **Several households per person, with an active household chosen per
  session.** This was rejected. Every request would carry a household the
  client chose, and with the app as the only lock, one place that trusts that
  choice without checking membership exposes another household. Moving to it
  later is additive, by dropping a uniqueness constraint and adding a switcher.
  Moving back from it would not be.
- **At most one household per person.** This was chosen.

The ingredient catalog had three options:

- **One global catalog that every household edits.** This was rejected,
  because one household's merge would rewrite another household's recipes and
  pantry, and its aisle edit would reorder another household's shopping lists.
- **A separate catalog per household, each seeded with its own copy.** This was
  rejected, because shared vocabulary would multiply, and fixing a seed entry
  would have to be done once per household.
- **A central catalog plus a per-household overlay, with promotion from an
  overlay into the central catalog.** This was chosen.

Promotion leaves a question about other households' overlay entries that
duplicate the promoted one:

- **Keep them, and suggest a merge to each household.** This was rejected.
  Every household would have to triage suggestions for what is plainly the
  same ingredient, and a dismissed suggestion would leave duplicates for good.
- **Keep them and do nothing.** This was rejected, because overlay matching
  comes first, so the duplicates would never converge on the central entry.
- **Merge them into the promoted entry automatically.** This was chosen. An
  ingredient is shared vocabulary rather than household data, and the
  operator reviews every promotion before it happens.

Three options were considered for sharing between households:

- **A live, read-only share.** This was rejected. Every recipe query would gain
  a "shared with me" path, rendering the shared recipe's lines would expose the
  sender's overlay entries, and revoking a share another household has already
  read is cosmetic.
- **Public or link sharing.** This was rejected. Access admits any email that
  authenticates, so "anyone with the link" means anyone with an email address.
- **Sending a copy to another household.** This was chosen. After the copy is
  made, each household again reads only its own rows.

Three options were considered for roles within a household:

- **Owner and member**, where only owners invite, remove members or change
  roles. This was rejected. A household is a set of people who trust each
  other, and the role column, owner checks and "at least one owner" rule would
  guard against a betrayal the owner does not expect.
- **Fine-grained permissions.** This was rejected as far beyond a household's
  needs.
- **All members equal.** This was chosen.

A household's last member can leave in one of three ways:

- **Leaving deletes the household.** This was rejected, because one click
  meant as "leave" would destroy every recipe, note and revision the
  household holds.
- **The household is kept with no members**, and the operator can later
  invite someone into it. This was rejected. It keeps data nobody can reach,
  and the operator page would have to list households, which widens what the
  operator sees.
- **The last member cannot leave** and must delete the household instead, as
  a separate act. This was chosen.

## Decision

### Tenancy

The tenant is the **household**. Every recipe-domain and shopping-domain row
belongs to exactly one household, and a request can read or change only the
data of a household its verified identity is a member of. A household has one
or more members, identified by their verified Access email.

An email belongs to **at most one household**, and the schema enforces this. A
request's household is derived from its verified identity alone. No request
names the household it acts in, and none can choose it. Accepting an
invitation while already a member of a household is refused, with a message
saying to leave the current household first.

### Privacy

A household's recipes, their notes, revisions and made-it log, its shopping
lists, its pantry and its meal plans are private to that household by
default. Sharing
between households is a wanted feature and goes through the sharing rules
below. Nothing becomes visible to another household except through an act of
sharing.

Meal planning (deferred by ADR-00011) is household data, like a shopping list.
Its shape is left to its own ADR.

### Sharing

Only **recipes** are shared, never shopping lists or pantry. A share is a
**copy sent to another household**:

- The sender names a recipient by the email of one of its members. The sender
  sees the same confirmation whether or not that email belongs to a household,
  so sharing does not reveal who is a member anywhere.
- The recipient household sees the share as pending, and any of its members
  may accept or decline it. Nothing enters a household's recipes without that
  acceptance.
- Accepting creates an independent recipe in the recipient household. It
  carries the recipe document and its source snapshot, unchanged (ADR-00004).
  It does not carry the sender's notes, revisions, made-it log or rating. The
  new recipe records where it came from: the sending household's name and the
  date.
- Ingredient lines are re-matched against the recipient's catalog. A line that
  pointed at a sender overlay entry matches central or the recipient's overlay,
  or else lands in the recipient's overlay as unreviewed. The sender's overlay
  is never exposed.
- Later edits on either side stay on that side. Sending again sends a new copy.

### Catalog

The ingredient catalog (ADR-00008) has two layers:

- **Central.** These are ingredients, units and equipment shared by every
  household, including today's seed. Households read the central layer but do
  not edit it.
- **Overlay.** Each household has its own overlay: the ingredients its
  ingestion or editing created (every unreviewed ingredient lands here), plus
  its own aliases and aisle overrides for central entries. One household's
  overlay is not visible to another.

Matching consults the household's overlay first and the central layer second.
A household's merge re-points only that household's rows. It may merge one
overlay entry into another, or an overlay entry into a central entry. It may
never merge two central entries.

**Promotion** moves an overlay entry into the central layer, so every household
can use it. Promotion keeps the entry's identity, so the promoting household's
rows need no rewrite. **Only the operator promotes.** A household may flag an
overlay entry as suggested for central, and the operator reviews it and
promotes it. The operator is also the only curator of the central layer. A
promoted entry's name becomes visible to every household, so the operator
decides whether to promote with that in mind.

Promotion also **merges duplicates automatically**. Another household's
overlay entry is a duplicate only when its normalised name or one of its
aliases exactly matches the promoted entry's name or one of its aliases.
That household's rows are re-pointed to the central entry. Its aliases and
its aisle are kept in its overlay as an alias set and an aisle override on
the central entry, so its matching and its shopping-list order do not change.
Before promoting, the operator sees how many duplicates will be merged. The
operator does not see which households they belong to. This is the one case
where a merge reaches beyond the household that started it.

### Members

Every member of a household has the same powers. Any member can:

- edit, delete and share the household's recipes;
- manage its shopping lists and pantry;
- invite people into the household;
- remove another member.

There are no roles.

Any member can leave, except the **last member**. The last member is refused,
with a message saying to delete the household instead. Deleting a household is
a separate action, open only to its last member. It needs a typed confirmation
of the household's name, and it removes all of the household's data.

### Admission

Cloudflare Access authenticates any email that can prove itself (for example
with a one-time PIN). It no longer limits who may enter, and the app still
verifies the Access JWT on every request (ADR-00003). Membership is held by the
app and decided by the app.

A verified email that is neither a member of a household nor named by a
pending invitation sees a "not a member" page and nothing else. No household
data, no catalog data and no counts are shown to it, and no route is reachable
by it except the health check, that page, and accepting an invitation. An
operator email is the one exception: it also reaches the operator's pages, so
the operator can issue the first household invitation before any household
exists.

Because the membership check is the only lock, every route carries a test made
as an authenticated non-member and as a member of a different household. Each
test asserts that the response contains no data from the household it does not
belong to.

### Invitations

Nobody joins or creates a household without an invitation addressed to their
email. There are two kinds:

- **Join invitation.** A member of a household invites an email into that
  household. Accepting it makes the invitee a member.
- **Household invitation.** The operator invites an email to start a new
  household. Accepting it creates the household with the invitee as its first
  member.

An invitation names the email it is for, and only a request verified as that
email can accept it. It expires, it can be used once, and the person who issued
it can revoke it before it is accepted.

The **operator** is defined in configuration (a list of emails), never in the
database, so operator status cannot be granted from inside the app. The
operator's pages offer only three things: issuing and revoking household
invitations, reviewing overlay entries suggested for central, and the
deployment settings (backup, storage and health). None of them shows a
household's data. Members who are not operators cannot reach them.

The deployment settings are operator-only because backup, storage and health
belong to the whole deployment, not to any household. Showing them to every
member was rejected: a member of one household would see, and could change,
state that affects every other household.

### Todoist

Todoist push (ADR-00009) is deferred, for two reasons. As built it uses one
token and one project for the whole deployment, so every household's list
would land in one account, which breaks the isolation this ADR rests on. And
nobody has yet asked for it: the scope was growing, and an integration with no
known user is where the line was drawn. The integration that eventually
returns may not be Todoist.

Two options were rejected:

- **Keep the one deployment-wide token.** Rejected because it sends one
  household's list into an account another household may own.
- **Add a per-household token now.** Rejected because it builds per-household
  secret storage, which no ADR covers, for a feature nobody has asked for.

It returns with its own ADR.

## Consequences

This reverses the single-user premise:

- **ADR-00003.** Access still authenticates, but the app now authorises: it
  maps the verified email to household membership and scopes every query to it.
- **ADR-00011.** "Invitations and multiple users" stops being a feature on top
  of existing data. It becomes a tenancy change to the schema. Todoist push
  leaves "In scope".
- **ADR-00009.** Todoist push is deferred (see Todoist above).

No deployment holds data yet, so the schema gains tenancy with nothing to
move. The first household is created like any other: the operator issues a
household invitation to themselves.

**Obligations:**

1. Amend ADR-00003 with a note at its Decision section naming this ADR. Set its
   Status to "Amended by ADR-00018" and update its index row.
2. Amend ADR-00011's "Deferred, planned later" invitations bullet the same way.
3. Amend the opening paragraph of `AGENTS.md`, which says "single-user".
4. `docs/operations.md` states that the Access application must admit any
   email that authenticates, and that admission is decided in the app.
5. Every route has a test made as an authenticated non-member and as a member
   of another household. The e2e route-coverage gate checks both, as it checks
   route coverage today.
6. The configuration key for operator emails is added to `internal/config`,
   `.env.example`, `compose.yaml` and the settings table in
   `docs/operations.md`.
7. Amend ADR-00009's Todoist push bullet and ADR-00011's "In scope" paragraph
   with a note naming this ADR. Set ADR-00009's Status to "Amended by
   ADR-00018" and update its index row.
