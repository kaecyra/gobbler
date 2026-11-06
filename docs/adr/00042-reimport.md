# ADR-00042: A saved recipe with a source URL is re-imported through ingestion's review screen, and saving writes a new revision

**Status:** Accepted

**Date:** 2026-11-06

## Context

The owner asked whether a recipe can be re-imported. It cannot. Importing the
URL again creates a second recipe and leaves the first with its journal,
made-it entries, history and shares. Parser improvements, ADR-00039's pictures
and ADR-00041's author notes never reach a recipe imported before they
existed. The decisions below were made one at a time with the owner; the
working record, with the evidence checked against the code, is
`docs/proposals/reimport.md`.

Ingestion (ADR-00006) already does everything a re-import needs up to the
save: it fetches, runs the rules, falls back to the LLM, and shows the draft
with per-field confidence, author-note rows and a picture picker. Its draft is
a proposal (ADR-00035, "Pending proposals") and its save creates a new recipe
with the source snapshot attached. A recipe's step and line keys are assigned
by the store at save and ingestion leaves them empty (ADR-00035, "Identity of
steps and ingredient lines"). The AGENTS.md boundary that an ingested recipe's
source snapshot is never modified is untouched by storing a second snapshot.

Options considered for what re-import does with the recipe:

- **"Import again" creates a new recipe,** and the person deletes the old one.
  Almost free now that deleting a recipe exists. Rejected: it loses the
  journal, notes, made-it history and shares, which are the data this project
  treats as the expensive thing to lose.
- **No re-import.** Rejected: older recipes would never gain pictures or
  author notes.
- **Re-import through ingestion, saving over the existing recipe.** Chosen.

Options considered for where the result is reviewed:

- **An edit proposal in the recipe editor, with a summary of what changes**
  (ADR-00035). The owner first ruled this and reversed it in the same session.
  Rejected: an imported draft carries no keys, so the summary has nothing to
  pair old and new parts on, and the editor loses the draft's confidence
  flags. Two pairings were considered and rejected for that reason: pairing by
  text and then position, and pairing by position alone (which shows every
  line below an insertion as changed, as ADR-00035 found). The owner's reason
  for dropping the summary is that the draft is reviewed whole on the ingest
  screen, so there is nothing to pair.
- **Three-way merge** against the version first imported, applying only what
  changed at the source. Rejected for now: it needs merge and conflict rules
  and a way to show conflicts. It can be added later if re-import is used often
  on edited recipes.
- **Fill in only what is missing** (picture, author notes, empty fields).
  Rejected: that is a backfill, and parser improvements to lines and steps
  would never reach old recipes.
- **Ingestion's review screen, with only the save changed.** Chosen, on the
  owner's instruction to use ingestion's whole flow. The screen keeps its
  confidence flags, author-note rows and picture picker.

Options considered for the picture:

- **Re-import never changes the picture.** Rejected: older recipes would stay
  without pictures unless a separate "Change picture" action were built, and
  ADR-00039 ruled out setting a picture from a typed URL.
- **The review screen's picker, as for an import.** ADR-00039 makes the person
  choose a candidate or "No picture". Rejected for re-import: a person who
  re-imports for the text would have to pick a picture to proceed, and "No
  picture" would be one careless click from deleting the one they have.
- **The picker plus "Keep current picture", which is the default.** Chosen.

Options considered for agents:

- **No MCP tool.** Rejected by the owner.
- **A tool that starts a re-import and leaves the draft held for a person.**
  Chosen. It is the same draft, held under ADR-00034's rule that every recipe
  write waits for review.

## Decision

- A saved recipe with a source URL can be re-imported. Re-import fetches the
  source through the guarded fetcher (ADR-00039, "Fetching"), stores the result
  as a new source snapshot beside the old one, and runs the full ingestion
  pipeline: rules first, the LLM fallback only after (ADR-00006). The old
  snapshot is not touched.
- A recipe may hold more than one source snapshot of a kind. The one-per-kind
  uniqueness on `source_snapshots` is dropped; a reader that wants one
  snapshot of a kind takes the newest, and the first stays as the original
  (owner, 2026-11-06, after the one-per-kind rule was found to block the
  second snapshot).
- The result is an ingestion draft that also records the recipe and the
  version it started from. It is reviewed on ingestion's review screen, with
  its confidence flags, author-note rows and picture picker. It is a pending
  proposal for that recipe: a recipe has at most one, so a re-import
  supersedes a pending agent edit proposal and the reverse (ADR-00035,
  "Pending proposals"). Re-import drafts do not count towards the 20-draft
  bound, which that record sets for drafts with no base recipe. A fetch or
  pipeline failure shows its message to the person and stores no draft.
- Only the save differs from an import. Saving writes a new revision of the
  existing recipe (ADR-00004) in place of creating a recipe, and attaches the
  new snapshot to it. Journal, made-it entries, shares and history stay
  attached, and the previous version restores from history. The save carries
  the recipe version the re-import started from and is refused if the recipe
  has moved on, as a stale editor save is.
- The save writes its revision with the change kind an accepted proposal uses
  (ADR-00035, "History"), and its provenance reads "Re-imported from <source
  host>", so History says what happened. It adds no change kind (dispatcher
  default, 2026-11-06).
- A pending re-import draft shows ADR-00035's pending-proposal banner on the
  recipe page, reading "A re-import is waiting for review". Its Review link
  goes to the ingest review screen. There are no inline marks, since nothing is
  paired. A stale draft is refused at save, and the banner then says the recipe
  changed since the re-import and offers Discard (dispatcher default,
  2026-11-06).
- The review page for a re-import carries a page-level callout, "Saving
  replaces the current recipe. Your earlier edits are kept in History.", shown
  only for a re-import.
- The picture picker (ADR-00039) offers "Keep current picture" beside the
  candidates and "No picture". Keeping the current picture is the default. The
  chosen picture is applied on save outside the revision, as ADR-00039 keeps
  pictures.
- The recipe page shows a "Re-import from source" link among the action links
  at its bottom, after History, Edit and Share and above the Delete recipe
  button. It shows only when the recipe has a source URL. It is a POST button
  styled like the other action links, because starting it writes a snapshot and
  a draft. It starts the fetch and lands on the review screen.
- An MCP tool starts a re-import for a recipe and creates the same draft. The
  draft is held for a person's review (ADR-00034); the agent never saves it.

## Consequences

This amends ADR-00035: its "Decision" ruling that ingestion's review screen is
the review surface for new recipes, and its "Pending proposals" ruling that
ingestion's drafts have no base recipe, both now have an exception for
re-import. It also amends ADR-00039, whose picker requires a choice among the
candidates and now offers "Keep current picture" as the default for a
re-import. Neither record is edited. The household's earlier edits are
replaced by the draft on save and recoverable only through History, which the
callout says.

**Obligations:**

1. Plan node i-reimport (#462), after r-author-notes (#395), adds the
   re-import action and its draft.
2. i-reimport stores a second source snapshot for the recipe and a test shows
   the first snapshot's content is unchanged and, after the save, both are
   attached to the recipe. Its migration drops the one-per-kind uniqueness on
   `source_snapshots`, and a test shows a single-snapshot read returns the
   newest of a kind.
3. i-reimport runs the rule-based path before any LLM call, with a test that
   the LLM is not called when the rules reach the confidence threshold.
4. Tests show a re-import supersedes a pending agent proposal for the same
   recipe and the reverse, and that a failed fetch stores no draft and shows
   its message.
5. A test saves a re-import and shows it writes a revision of the existing
   recipe and creates no second recipe, and that the recipe's journal,
   made-it entries and shares are unchanged and the previous version restores
   from history.
6. A test starts a re-import, edits the recipe in the editor, saves the
   re-import, and shows the save is refused with the recipe unchanged.
7. The re-import review screen keeps its confidence flags and author-note
   rows. Tests show the picker defaults to "Keep current picture", that saving
   with the default leaves the picture as it was, that choosing a candidate
   replaces it and deletes the previous object, that "No picture" removes it,
   and that a discarded or superseded re-import leaves no
   `drafts/<proposal_id>/` objects.
8. The review screen shows the callout "Saving replaces the current recipe.
   Your earlier edits are kept in History." for a re-import and not for an
   import, with e2e coverage of both.
9. i-reimport adds the "Re-import from source" POST button at the bottom of
   the recipe page, hidden for a recipe with no source URL, with e2e coverage
   of both.
10. An MCP tool test starts a re-import and shows the draft is held, the
    recipe and its picture are unchanged, and no MCP tool can save the draft.
11. A test saves a re-import and shows the revision has the change kind an
    accepted proposal uses, its provenance reads "Re-imported from <source
    host>" and History shows it, and the migrations directory gains no file
    for this node's change kind (dispatcher default, 2026-11-06).
12. E2e tests show a pending re-import draft puts the banner "A re-import is
    waiting for review" on the recipe page with a Review link to the ingest
    review screen and no inline marks, and that after the recipe is edited the
    banner says the recipe changed since the re-import and offers Discard
    (dispatcher default, 2026-11-06).
