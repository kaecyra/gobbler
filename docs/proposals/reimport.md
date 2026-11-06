# Open decisions: re-importing a recipe from its source

Owner question, 2026-11-06: "is there a way to re-import a recipe?" There is not. Worked through with
`/overseer:decide`. Ruled in [ADR-00042](../adr/00042-reimport.md), which carries the obligations; the work is plan node
i-reimport (#462).

## Verified facts

- Nothing re-fetches the source of a saved recipe. The only route today is importing the URL again,
  which creates a second recipe and leaves the first with its journal, made-it entries, history and
  shares.
- ADR-00035 stores a recipe awaiting review as a proposal. An edit proposal has a base recipe and the
  version it was made against, and it is reviewed in the recipe editor, pre-filled, with a summary of
  what changes. Saving writes a revision (ADR-00004). Steps and ingredient lines carry keys that are
  never reused, which is what pairs old and new parts in that summary.
- AGENTS.md: "An ingested recipe's source snapshot is never modified." A second snapshot beside the
  first modifies nothing.
- The picture picker lives on the ingest review screen only (ADR-00039).
- ADR-00039 does not backfill pictures on recipes imported before it, and author notes (ADR-00041)
  will be missing from every recipe imported before r-author-notes ships.

## D1 — A re-import is an edit proposal against the current version

**Decided.** A saved recipe with a source URL can be re-imported. Re-import fetches the source again,
stores the result as a new snapshot beside the old one, and runs the usual pipeline: rules first, the
LLM fallback only after (ADR-00006). The result becomes an edit proposal against the recipe's current
version (ADR-00035), reviewed in the editor with the change summary. The journal, made-it entries,
shares and history stay attached, and the previous version can be restored from history.

**Why.** It keeps the household's data, which this project treats as the expensive thing to lose, and
it reuses the review path that already exists for agent edits, so the LLM never changes a recipe
without review.

Rejected:
- **"Import again" creates a new recipe**, and the person deletes the old one. Almost free now that
  delete exists, but it loses the journal, notes, made-it history and shares.
- **No re-import.** Parser improvements, pictures and author notes would never reach older recipes.

**Obligations:**

1. An ADR records re-import as an edit proposal, citing ADR-00035 and ADR-00006.
2. A plan node, after r-author-notes, adds the re-import action and its proposal.

## D2 — The proposal replaces the whole recipe; the household's own edits show as changes

**Decided.** A re-import proposes the freshly imported recipe as a whole against the current version.
The editor's change summary shows every difference, including the household's own earlier edits being
reverted; the person keeps an edit by re-editing that field before saving. History can restore any
earlier version.

**Why.** It is ADR-00035's edit proposal unchanged, and it never hides a change from the reviewer.

Rejected:
- **Three-way merge** against the version first imported, applying only what changed at the source.
  The household's edits would survive untouched, but it needs merge and conflict rules and a way to
  show conflicts, which is more than the partial acceptance ADR-00035 already rejected as the most to
  build. It can come later if re-import is used often on edited recipes.
- **Fill in only what is missing** (picture, author notes, empty fields). Safe and small, but it is a
  backfill, not a re-import: parser improvements to lines and steps would never reach old recipes.

**Obligations:**

1. The re-import ADR states that the proposal is the whole imported recipe against the current version.

## D3 — The editor review offers the picture picker for a re-import

**Decided.** When the proposal under review comes from a re-import, the editor shows the picture picker
(ADR-00039) with the fresh candidates, plus a choice that keeps the current picture and "No picture".
Keeping the current picture is the default. The picker is shared by the ingest review screen and the
editor; it joins the editor's form through `editor.FormID`.

**Why.** Backfilling pictures on recipes imported before ADR-00039 is one of the two main reasons to
re-import (author notes are the other), and the picker already exists.

Rejected:
- **Re-import never changes the picture.** Simpler, but older recipes would stay without pictures
  unless a separate "Change picture" action were built, and ADR-00039 ruled out setting a picture from
  a typed URL.

**Obligations:**

1. The re-import proposal carries picture candidates as an ingest draft does, and the editor review
   renders the shared picker for it, defaulting to the current picture.

## D4 — "Re-import from source" is an action link at the bottom of the recipe page

**Decided.** The recipe page gains a "Re-import from source" link among the action links at its bottom,
after History, Edit and Share and above the Delete recipe button (#454). It shows only when the recipe
has a source URL; a recipe from a photo or pasted text has nothing to fetch again. Following it starts
the fetch and lands on the editor review of the proposal.

**Why.** The recipe's other whole-recipe actions already live there, and the owner placed it there.

**Obligations:**

1. The re-import node adds the link, hidden for a recipe with no source URL, with e2e coverage of both.
