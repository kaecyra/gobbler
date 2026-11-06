# Open decisions: re-importing a recipe from its source

Owner question, 2026-11-06: "is there a way to re-import a recipe?" There is not. Worked through with
`/overseer:decide`. Ruled in [ADR-00042](../adr/00042-reimport.md), which carries the obligations; the work is plan node
i-reimport (#462).

## Verified facts

- Nothing re-fetches the source of a saved recipe. The only route today is importing the URL again,
  which creates a second recipe and leaves the first with its journal, made-it entries, history and
  shares.
- ADR-00035 stores a recipe awaiting review as a proposal, one pending per recipe. Ingestion's draft of
  a new recipe is a proposal with no base recipe, reviewed on ingestion's review screen; confirming
  creates a recipe. An edit proposal has a base recipe and the version it was made against, and is
  reviewed in the recipe editor. Saving writes a revision (ADR-00004).
- ADR-00035: steps and ingredient lines get their keys from the store at save, and ingestion leaves
  them empty. An imported draft therefore has no keys to pair with a recipe's current parts. (The
  first version of this record assumed keys paired old and new parts; they cannot for an import.)
- AGENTS.md: "An ingested recipe's source snapshot is never modified." A second snapshot beside the
  first modifies nothing.
- ADR-00035: a draft's snapshot is written when the draft is made, with no recipe, and attached once on
  confirm.
- The ingest review screen carries the picture picker (ADR-00039), confidence flags and, after
  r-author-notes, author-note rows. The editor carries none of them.
- ADR-00039 does not backfill pictures on recipes imported before it, and author notes (ADR-00041)
  will be missing from every recipe imported before r-author-notes ships.
- ADR-00034 holds every recipe write from MCP for review.

## D1 — A re-import runs ingestion's whole flow, and only the save differs

**Decided.** A saved recipe with a source URL can be re-imported. Re-import fetches the source through
the guarded fetcher, stores the result as a new snapshot beside the old one, and runs the full ingest
pipeline: rules first, the LLM fallback only after (ADR-00006). The draft is reviewed on the ingest
review screen. Only the save differs: it writes a new revision of the existing recipe instead of
creating one, and attaches the new snapshot to it. The journal, made-it entries, shares and history stay
attached, and the previous version can be restored from history. The save carries the recipe version the
re-import started from and is refused if the recipe has moved on, as a stale editor save is. The draft
is a pending proposal for the recipe, so it supersedes a pending agent edit proposal and the reverse
(ADR-00035). It does not count towards the 20-draft bound. The review page carries a page-level callout,
"Saving replaces the current recipe. Your earlier edits are kept in History."

Dispatcher default, 2026-11-06 (the owner can overturn it on the PR): the save writes its revision with
the change kind an accepted proposal uses, with provenance "Re-imported from <source host>", so History
says what happened. No new change kind. (A schema change is needed after all: `source_snapshots` allowed
one snapshot per recipe per kind, so the owner ruled on 2026-11-06 to drop that uniqueness, with readers
taking the newest; ADR-00042 records it.)

Owner: "use the ingest's whole flow, just change what happens on save."

**Why.** It keeps the household's data, which this project treats as the expensive thing to lose, and it
keeps the confidence flags, author-note rows and picture picker that only the ingest screen has.

Rejected:
- **"Import again" creates a new recipe**, and the person deletes the old one. It loses the journal,
  notes, made-it history and shares.
- **No re-import.** Parser improvements, pictures and author notes would never reach older recipes.
- **An edit proposal reviewed in the recipe editor with a change summary** (ADR-00035's path). This was
  the first ruling; the owner reversed it mid-session. The imported draft has no keys to pair on, the
  editor loses the confidence flags, and the draft is better reviewed whole on the ingest screen.
- **Pairing by text and then position** to give the summary something to show. Rejected with the summary.
- **Pairing by position alone.** A draft that inserts one line would show every line below it as changed
  (ADR-00035 found this). Rejected with the summary.
- **Three-way merge** against the version first imported. It needs merge and conflict rules and a way to
  show conflicts. It can come later if re-import is used often on edited recipes.
- **Fill in only what is missing** (picture, author notes, empty fields). A backfill, not a re-import:
  parser improvements to lines and steps would never reach old recipes.

**Obligations:**

1. An ADR records re-import as ingestion's flow with a different save, citing ADR-00006 and amending
   ADR-00035 by section.
2. Plan node i-reimport, after r-author-notes, adds the re-import action and its draft.
3. A test shows the first snapshot is unchanged and both are attached after the save.
4. A test shows the rule-based path runs before any LLM call.
5. Tests show a re-import supersedes a pending agent proposal and the reverse, and that a failed fetch
   stores no draft.
6. A test shows the save writes a revision of the existing recipe, creates no second recipe, leaves the
   journal, made-it entries and shares unchanged, and the previous version restores from history.
7. A test shows the save is refused if the recipe was edited after the re-import started.
8. The review screen shows the callout for a re-import and not for an import, with e2e coverage of both.
9. A test shows the save's revision has the change kind an accepted proposal uses, with provenance
   "Re-imported from <source host>" shown in History, and no migration adds a change kind.

## D2 — No change summary and no pairing

**Decided.** The review shows the fresh draft, not a diff. The household's own earlier edits are replaced
on save and recovered through History, which the callout says.

**Why.** Follows from D1: the draft is reviewed whole on the ingest screen, and an imported draft has no
keys to pair on.

Rejected: the three-way merge, fill-in-only and pairing options under D1.

**Obligations:**

1. The ADR states that the review shows the draft and not a diff, and that earlier edits live on in
   History.

## D3 — The picker gains "Keep current picture", the default for a re-import

**Decided.** The ingest review screen's picture picker (ADR-00039) offers the fresh candidates, "No
picture", and for a re-import "Keep current picture", which is the default. ADR-00039 is amended for
re-import.

**Why.** Backfilling pictures on recipes imported before ADR-00039 is one of the two main reasons to
re-import (author notes are the other), and the picker already exists. A person re-importing for the
text should not have to choose a picture to proceed, and "No picture" is one click from deleting the one
they have.

Rejected:
- **Re-import never changes the picture.** Older recipes would stay without pictures unless a separate
  "Change picture" action were built, and ADR-00039 ruled out setting a picture from a typed URL.
- **The editor's review offers the picker, joined to the form through `editor.FormID`.** The first
  version of this decision, reversed with D1.

**Obligations:**

1. The re-import draft carries picture candidates as an ingest draft does. Tests show the picker
   defaults to "Keep current picture", saving with the default leaves the picture as it was, choosing a
   candidate replaces it and deletes the previous object, "No picture" removes it, and a discarded or
   superseded re-import leaves no `drafts/<proposal_id>/` objects.

## D4 — "Re-import from source" is an action link at the bottom of the recipe page

**Decided.** The recipe page gains a "Re-import from source" link among the action links at its bottom,
after History, Edit and Share and above the Delete recipe button (#454). It is a POST button styled like
the other action links, because starting it writes a snapshot and a draft. It shows only when the recipe
has a source URL; a recipe from a photo or pasted text has nothing to fetch again. Pressing it starts the
fetch and lands on the review screen.

**Why.** The recipe's other whole-recipe actions already live there, and the owner placed it there.

Dispatcher default, 2026-11-06 (the owner can overturn it on the PR): a pending re-import draft shows
ADR-00035's pending-proposal banner on the recipe page, reading "A re-import is waiting for review". Its
Review link goes to the ingest review screen. There are no inline marks, since there is no pairing. A
stale draft is refused at save, and the banner then says the recipe changed since the re-import and
offers Discard.

**Obligations:**

1. The re-import node adds the button, hidden for a recipe with no source URL, with e2e coverage of
   both.
2. E2e tests show a pending re-import draft puts the banner on the recipe page with a Review link to the
   ingest review screen and no inline marks, and that after the recipe is edited the banner says the
   recipe changed since the re-import and offers Discard.

## D5 — An MCP tool starts a re-import

**Decided.** An MCP tool starts a re-import for a recipe and creates the same draft. The draft is held
for a person's review (ADR-00034); the agent never saves it.

**Why.** Agents already propose edits over MCP, and ADR-00034 already holds every recipe write for
review, so the draft changes nothing until a person saves it.

Rejected:
- **No MCP tool.** The owner chose to allow it.

**Obligations:**

1. An MCP tool test starts a re-import and shows the draft is held, the recipe and its picture are
   unchanged, and no MCP tool can save the draft.
