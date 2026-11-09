# ADR-00035: Recipes awaiting review are stored as proposals, and an edit is reviewed in the recipe editor

**Status:** Accepted

**Date:** 2026-11-04

## Context

ADR-00034 holds every recipe write from MCP for review, but the only review
screen is ingestion's (ADR-00006), and confirming there creates a new recipe.
An agent has no way to propose a change to a recipe that already exists (#353).

An edit differs from an ingest in kind. It has a base recipe and the version it
was made against, and it has no source snapshot, parser method or per-field
confidence. The recipe editor already loads an existing recipe, refuses a save
against a version that has moved on, proposes new catalog entries and writes a
revision (ADR-00004).

Options considered for where an edit is reviewed:

- **Extend the ingestion review screen.** One place for everything an agent
  proposes. Rejected: that screen is shaped around an ingestion draft, and
  confirming an edit from it would need a second confirm path duplicating the
  editor's version check and stale handling.
- **A diff-only page with accept or reject per change.** The clearest view, and
  it lets a person take part of a proposal. Rejected: partial acceptance must
  keep the recipe valid, and a kept step that uses a dropped ingredient, or a
  dependency on a dropped step, breaks it. Every combination would need its own
  validation, and it is the most to build.
- **The recipe editor, pre-filled with the proposal, with a summary of the
  changes.** Chosen.

Showing what a proposal changes means pairing each part of the proposed recipe
with the part of the current one it replaces. Every save rewrites a recipe's
child rows, so row ids do not survive an edit and cannot be that pairing. Steps
carry a key unique within one version (ADR-00004's step dependency graph), but
a new step may take the key of one removed earlier, so a key does not name the
same step across versions. Ingredient lines carry no key at all.

Options considered for pairing:

- **Pair lines by position.** No schema change. Rejected: a proposal that
  inserts one line shows every line below it as changed.
- **Give every step and ingredient line a key that is never reused.** Chosen.

Options considered for minting those keys:

- **A counter per recipe** (`s7`, `l12`). Short and readable. Rejected: one
  more piece of state to keep right, and the editor, ingestion and MCP would
  all have to defer key choice to the store under its lock.
- **A random key with a type prefix and the recipe's id.** The recipe would be
  readable from a bare key. Rejected: a clone (ADR-00004) would carry keys
  naming the wrong recipe or need every key rewritten, a new recipe has no id
  until it is inserted, and keys are always stored beside their recipe id.
- **A random key with a type prefix.** Chosen. Nothing to coordinate, one rule
  for every writer, and the prefix makes a step key passed where a line key
  belongs fail visibly.

Options considered for keys inside revisions written before this change:

- **Rewrite them to the new keys,** as ingredient merges already rewrite
  ingredient references inside revisions. Rejected as unneeded: no household
  holds recipes yet, so there is no history to pair.
- **Leave them.** Chosen. Comparison against such a revision pairs by position.

Ingestion holds drafts only in memory: they expire after an hour and a restart
drops them. That suits a person watching an ingest, not an agent proposing an
edit for someone to review the next day.

Options considered for storing a proposal:

A proposal is a whole recipe that is not current: made, compared and
validated whole, and then accepted, rejected or superseded. Nothing queries its
content except the ingredient merge, which must keep its catalog references
valid.

- **A state column on the recipe and every child table.** Every element is a
  first-class row. Rejected: the live tables hold one current version, each
  step key unique within its recipe, so a proposed change to a step collides
  with the live step; every child table and every read would change. State per
  element is also partial acceptance, rejected above.
- **A shadow recipe: a recipe row marked proposed, pointing at the recipe it
  changes.** The strongest alternative. Loading, validation, the step graph and
  the editor work unchanged, the ingredient merge re-points it with the live
  lines, and foreign keys guard its catalog references. Rejected: every query
  over recipes — library, search, fields, video, MCP lookups, shopping entries,
  share links, the journal — and every future one must exclude shadows, and a
  missed filter fails silently, as a duplicate in the library or unapproved
  quantities on a shopping list. That cost never ends.
- **The revisions table.** It already holds whole recipe documents with an
  author, a kind, an ingredient index and merge re-pointing. Rejected:
  revisions are immutable history (ADR-00015) and a proposal's state changes;
  a proposal's base version collides with the revision written when it is
  accepted; and history listing, version reads and restore would all need
  filters. "Revision" would mean two things.
- **A proposals table holding the proposed recipe as one document**, in the
  revision document format. Chosen. No existing or future query over live
  recipes or history can see a proposal. Its one extra duty, re-pointing its
  ingredient references on a merge, fails loudly if forgotten: an index of
  those references with a foreign key to the catalog blocks the merge's delete
  rather than leaving a dangling id. The cost is paid once — that index, the
  merge hook generalised to both document tables, and the comparison in Go,
  which the editor's summary needs anyway. A proposal's content cannot take
  part in SQL. Should proposals ever need to behave as recipes (cooked from,
  searched), the shadow recipe is the better model and this should be
  revisited.

Ingestion's drafts of new recipes have the same weakness: an agent importing
overnight loses its draft before anyone reviews it.

Options considered for ingestion's drafts:

- **Never move them.** Ingesting again is the recovery. Rejected: it does not
  help an import nobody was watching.
- **Move them later, in their own change.** Keeps the edit work to one
  concern. Rejected by the owner: one storage model for everything awaiting
  review, from the start.
- **Move them now.** Chosen. A draft of a new recipe is a proposal with no base
  recipe.

A draft of a new recipe carries the source it was parsed from, and a source
snapshot belongs to a recipe (ADR-00004), which a draft does not have yet.

Options considered for a durable draft's source:

- **Keep the source on the proposal and write the snapshot on confirm.** The
  snapshot rules stay as they are. Rejected: the content is held in two places,
  and the snapshot is a copy made later rather than the thing captured.
- **Snapshots belong to the proposal, permanently.** Written once and never
  moved. Rejected: it changes what every snapshot hangs off, and every snapshot
  read would need a second path.
- **Write the snapshot when the draft is made, without a recipe, and attach it
  to the recipe on confirm.** Chosen. The content is written once and never
  changes; attaching it to the recipe it produced is not a change to what was
  captured.

Drafts in memory expire after an hour and, past a per-household bound, the
oldest is dropped. Durable proposals need their own lifetime.

Options considered for a pending proposal nobody acts on:

- **Drop the oldest past a bound, as now.** Rejected: silent loss of work is
  what durable storage was meant to end.
- **Never expire.** Rejected: an abandoned proposal would sit forever.
- **Expire after a year.** Chosen. Long enough that no proposal a household
  means to review is lost to the timer.

Edit proposals bound themselves: one pending per recipe. Drafts of new
recipes do not, each can hold a page of up to several megabytes, and they now
live a year, so a looping agent or a leaked key could fill the disk and its
backups at the rate limit.

Options considered for bounding pending drafts of new recipes:

- **No bound.** Rejected: the rate limit slows growth but does not bound it.
- **Drop the oldest past a bound.** Rejected: silent loss, as above.
- **Refuse a new draft past a bound of 20 per household.** Chosen. Nothing is
  lost, the refusal says why, and a household reviewing its drafts never
  meets it.

Options considered for a finished proposal (rejected, superseded, expired):

- **Keep the row indefinitely as an audit record.** Rejected by the owner.
- **Prune after 60 days.** Chosen. Long enough to answer "what did the agent
  propose that we turned down", then the row and any snapshot it holds go.

Options considered for showing a pending proposal on the recipe page:

- **Not at all; a library badge and a list of proposals only.** Rejected: a
  proposal is easy to miss and sits pending for weeks.
- **A banner only, with the comparison in the editor.** Least to build.
  Rejected: the person cannot judge the change without opening the editor.
- **Inline: a banner with the agent's reason, and the changed elements marked
  in place.** Chosen. It shows the change where it matters and reuses the
  comparison the editor shows. A person who needs the recipe now, not a
  review, can switch the marks off.

A proposal is made against one version. If a person saves the recipe before
the proposal is reviewed, the proposal still holds that version's content, and
accepting it would silently undo the person's edit.

Options considered for a proposal whose recipe has moved on:

- **Show it as it stands against the live recipe.** Rejected: the person's own
  edit appears as the agent's change, and one careless save reverts it.
- **Merge by key against the version it was made from.** Each side's change to
  an element survives, and an element both changed is a conflict for the
  person to settle. Keeps unrelated work on both sides. Rejected for now:
  substantial code and edge cases (a step deleted on one side and edited on
  the other, changed dependencies) for an event that should be rare in one
  household. It can be added later without changing anything decided here.
- **A stale proposal cannot be accepted.** Chosen. Nothing can be lost, and the
  editor's version check already refuses a save against an old version.

An accepted proposal is saved as an ordinary edit, and a revision records only
the person who saved it and that it was an edit (ADR-00004). Who proposed the
change, through which credential, and why, would be lost from history.

Options considered for what history records:

- **Nothing new.** No work. Rejected: "who halved the salt, and why" has no
  answer in history.
- **Copy the provenance onto the revision.** A revision stands alone.
  Rejected: the facts are stored twice, and the revision table grows columns
  only one kind of change uses.
- **A distinct kind of change, with the revision referring to the stored
  proposal.** Chosen. Provenance lives once, on the proposal.

Options considered for several proposals against one recipe:

- **Allow many pending at once.** Rejected: they can contradict each other, and
  accepting one makes the rest stale.
- **One pending proposal per recipe; a newer one supersedes it.** Chosen.

## Decision

- A person reviews an agent's edit in the recipe editor (ADR-00012's `editor`
  area), opened with the proposed recipe filled in and a summary of what it
  changes against the current version shown above the form.
- The person may change the proposal before saving. Saving it is an ordinary
  editor save, validated as a whole recipe. Partial acceptance is done by
  editing the form, never by accepting changes one at a time.
- Ingestion's review screen stays the review surface for new recipes.

### Identity of steps and ingredient lines

- Every step and every ingredient line has a key, unique within its recipe
  for the recipe's whole life. A key is kept across edits and is never given
  to a different step or line, even after the one holding it is removed.
- A key is `stp-` for a step or `lin-` for an ingredient line, followed by 8
  random lowercase Crockford base32 characters, for example `stp-k3f9x2qa`.
  It carries no recipe id.
- The store assigns a key to any step or line saved without one. No writer
  mints keys itself: the editor, ingestion and MCP leave a new element's key
  empty. Ingestion's draft is unchanged by this, so it stays deterministic
  (ADR-00006).
- The revision document carries each key, so any two versions of a recipe can
  be paired element by element.
- Revisions written before keys existed are not rewritten. Comparing against
  one pairs elements by position.

### Pending proposals

- A proposal is stored durably in its own table, outside the live recipe
  tables, as one whole document. It records the recipe and version it was made
  against; its state (pending, accepted, rejected, superseded, expired); the
  reason an agent gave, if one made it; and its provenance: the person, the
  credential that made it if any (with the never-reused snapshot the credential
  write logs use), the MCP tool if any, and the time.
- Ingestion's drafts of new recipes are proposals too, with no base recipe,
  and are reviewed on ingestion's review screen as now. Confirming one records
  the recipe it created on the proposal. The in-memory draft table is removed.
- A recipe has at most one pending proposal. A new proposal for the same
  recipe supersedes the pending one, which is kept with its state set to
  superseded.
- A proposal's document is in the format of its kind: the ingestion draft,
  with its per-field confidence, LLM marks and parse method, for a new recipe;
  the recipe document for an edit.
- A draft's source snapshot is written when the draft is made, with no recipe,
  and the proposal refers to it. Confirming attaches it to the new recipe in
  the same transaction. Attaching happens once: it sets the recipe only on a
  snapshot that has none. A snapshot with no recipe may be deleted; one
  attached to a recipe goes only when the recipe is deleted.
- An uploaded photo stays in memory only, as before; it is never stored with
  a draft. Asking the LLM again on a draft whose photo is gone
  uses the OCR text alone, and the review screen says the photo is no longer
  available.
- A household holds at most 20 pending drafts of new recipes. A new draft past
  that is refused — on the ingest form and over MCP alike — with a message
  saying drafts are waiting for review; no draft is dropped to make room. Edit
  proposals do not count towards it.
- A pending proposal expires 365 days after it was made; its state becomes
  expired.
- A rejected, superseded or expired proposal is deleted 60 days after it
  reached that state, together with its source snapshot if no recipe holds
  it. An accepted proposal is never pruned (see History).
- No read path outside proposal review and display reads the proposals table.

  > Amended 2026-11-09 by [ADR-00048](00048-agent-driven-import.md) (an
  > import's duplicate check reads pending drafts' source addresses) and
  > [ADR-00049](00049-pending-drafts-listed-and-counted.md) (the menu counts
  > the household's pending drafts on every signed-in page, and the page after
  > a confirm or discard names the draft just ended).
- The ingredient ids inside a proposal are indexed with a foreign key to the
  catalog, as revision documents are, and an ingredient merge re-points them
  with the same code that re-points revisions, generalised to both.

### On the recipe page

- The household's recipe view shows a pending proposal inline: a banner with
  the agent's reason and provenance, and each changed step, line or field
  marked in place with its old and new values. The banner offers Review, which
  opens the editor, and Reject.
- A toggle switches the marks off and shows the live recipe alone, leaving a
  compact banner so the proposal is not forgotten. The toggle lasts for that
  visit; the next visit shows the marks again.
- Cook mode and shared links show the live recipe only, never a proposal.

### A recipe changed after the proposal

- A pending proposal is stale when the recipe's version is no longer the one
  it was made against. Staleness is computed from the versions, not stored.
- A stale proposal cannot be accepted. The recipe page shows a banner saying it
  is out of date, with no inline marks and with Reject as the only action. The
  editor refuses to open it for review, and a save that races past that check
  is refused by the version check.
- An agent reading its proposal over MCP is told it is stale and the recipe's
  current version, so it can propose again; the new proposal supersedes it.

### History

- Accepting a proposal writes the revision with the change kind `proposal`,
  and that revision refers to the proposal. Its author is the person who
  accepted it, as for any edit.
- An accepted proposal, of an edit or of a new recipe, is kept for as long as
  its recipe. Deleting the recipe
  deletes its proposals with its history (ADR-00015); nothing else deletes an
  accepted proposal.
- The history page shows, for such a revision, the agent's reason and the
  credential that made the proposal. Where the person changed the proposal
  before saving, it can show those changes by comparing the stored proposal
  with the saved version; no further record is kept for that.

## Consequences

This amends ADR-00004, obligation 4. A source snapshot's content still has no
update path. The store may attach a snapshot that has no recipe to one recipe,
once, and may delete a snapshot that has no recipe. The AGENTS.md boundary
that a snapshot is never modified holds: its content never changes.

There are two review surfaces: ingestion's for a new recipe, the editor's for a
change to an existing one.

**Obligations:**

1. #353's lane names the editor and recipe view areas and drops the ingestion
   review area.
2. Ingredient lines gain a stored key under a schema migration, and existing
   step and line rows are given keys in the new format by it. A test saves a
   recipe, removes a step and a line, adds new ones, and shows the new ones get
   keys never held before in that recipe.
3. The editor's step-id assignment, which can hand a new step the id of a
   removed one, is removed; a test shows a step added in the editor is saved
   with a store-assigned `stp-` key.
4. The revision document encodes every step and line key; a test round-trips
   them through a revision and a restore (ADR-00004, obligation 3).
5. A proposal survives a restart: a test stores one, opens a new store on the
   same database, and loads it with its state, reason and provenance.
6. A test holds a pending proposal that changes an ingredient and shows search
   and shopping-list merging do not see the change.
7. A test makes two proposals for one recipe and shows the first ends
   superseded and only the second is pending.
8. End-to-end tests cover the recipe page with a pending proposal: the marks
   show, the toggle hides them and keeps the banner, Reject leaves the recipe
   unchanged, and cook mode and a share link show no proposal.
9. A test makes a proposal, saves the recipe in the editor, and shows the
   proposal is reported stale on the recipe page and over MCP, cannot be
   opened for review, and that a review save racing the edit is refused with
   the recipe unchanged.
10. A test accepts a proposal and shows the revision written has change kind
    `proposal`, refers to the proposal, and names the accepting person as its
    author; the history page shows the agent's reason and credential.
11. Restoring a revision written by an accepted proposal round-trips the full
    recipe (ADR-00004, obligation 3).
12. Source snapshots allow no recipe under a schema migration. Tests show a
    snapshot attaches to a recipe once and a second attach is refused, a
    snapshot with no recipe can be deleted, an attached one cannot except by
    deleting its recipe, and the store still exposes no way to change a
    snapshot's content.
13. Tests with a controlled clock show a pending proposal expires at 365 days;
    a rejected, superseded or expired one is deleted at 60 days with its
    unattached snapshot; an accepted one and its attached snapshot survive.
14. A test holds 20 pending drafts of new recipes for one household and shows
    the 21st is refused on the ingest form and over MCP, no draft is dropped,
    another household is unaffected, and an edit proposal is still accepted.
15. A test confirms a draft of a new recipe from an uploaded photo after the
    photo has left memory, and shows asking the LLM again runs on the OCR text
    and the review screen says the photo is no longer available.
16. A test merges an ingredient that a pending proposal names and shows the
    proposal now names the winner and still loads. The revision re-pointing
    code is generalised to both document tables, not copied.
