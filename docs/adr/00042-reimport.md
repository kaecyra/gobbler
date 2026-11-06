# ADR-00042: A saved recipe with a source URL is re-imported as an edit proposal against its current version

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

ADR-00035 already gives a way to review a whole replacement of an existing
recipe: an edit proposal holds the proposed recipe against the version it was
made against, the recipe editor shows it pre-filled with a summary of what
changes, and saving writes a revision (ADR-00004). Steps and ingredient lines
carry keys that are never reused, which pairs old and new parts in that
summary. The AGENTS.md boundary that an ingested recipe's source snapshot is
never modified is untouched by fetching a second snapshot.

Options considered for what re-import does with the recipe:

- **"Import again" creates a new recipe,** and the person deletes the old one.
  Almost free now that deleting a recipe exists. Rejected: it loses the
  journal, notes, made-it history and shares, which are the data this project
  treats as the expensive thing to lose.
- **No re-import.** Rejected: older recipes would never gain pictures or
  author notes.
- **An edit proposal against the current version.** Chosen. It reuses the
  review path agent edits take, so the LLM never changes a recipe without
  review (ADR-00006).

Options considered for how much of the recipe the proposal replaces:

- **Three-way merge** against the version first imported, applying only what
  changed at the source. The household's edits would survive untouched.
  Rejected for now: it needs merge and conflict rules and a way to show
  conflicts, which is more than the partial acceptance ADR-00035 already
  rejected as the most to build. It can be added later if re-import is used
  often on edited recipes.
- **Fill in only what is missing** (picture, author notes, empty fields).
  Rejected: that is a backfill, and parser improvements to lines and steps
  would never reach old recipes.
- **The whole imported recipe against the current version.** Chosen. It is
  ADR-00035's edit proposal unchanged, and it hides no change from the
  reviewer.

Options considered for the picture:

- **Re-import never changes the picture.** Rejected: older recipes would stay
  without pictures unless a separate "Change picture" action were built, and
  ADR-00039 ruled out setting a picture from a typed URL.
- **The editor review offers the picker of ADR-00039.** Chosen. Backfilling
  pictures is one of the two main reasons to re-import, and the picker exists.

## Decision

- A saved recipe with a source URL can be re-imported. Re-import fetches the
  source again, stores the result as a new source snapshot beside the old one,
  and runs the ingestion pipeline: rules first, the LLM fallback only after
  (ADR-00006). The old snapshot is not touched.
- The result is stored as an edit proposal against the recipe's current
  version (ADR-00035), made by the person who started it, with no agent
  reason. It follows that record's rules for proposals: at most one pending per
  recipe, so a re-import supersedes a pending agent proposal; stale once the
  recipe changes; and the new snapshot attaches to the recipe when the proposal
  is accepted. A fetch or pipeline failure shows its message to the person and
  stores no proposal.
- The proposal is the freshly imported recipe as a whole. The editor's change
  summary shows every difference, including the household's own earlier edits
  being reverted. The person keeps an edit by making it again in the form
  before saving, and history restores any earlier version. Journal, made-it
  entries, shares and history stay attached to the recipe.
- When the proposal comes from a re-import, the editor review shows the
  picture picker (ADR-00039) with the fresh candidates, a choice that keeps
  the current picture, and "No picture". Keeping the current picture is the
  default. The candidates are stored and cleaned up as a draft's are. The
  picker is shared by the ingest review screen and the editor, and joins the
  editor's form through `editor.FormID`. The chosen picture is applied when the
  proposal is saved, outside the revision, as ADR-00039 keeps pictures.
- The recipe page shows a "Re-import from source" link among the action links
  at its bottom, after History, Edit and Share and above the Delete recipe
  button. It shows only when the recipe has a source URL. It is a POST button
  styled like the other action links, because starting it writes a snapshot and
  a proposal; it starts the fetch and lands on the editor review of the
  proposal.

## Consequences

This amends ADR-00035, whose edit proposal now may carry a new source
snapshot and may be made by a person, and ADR-00039, whose picker now also
appears in the editor and may default to the current picture. Neither record is
edited. Re-import is not a backfill of pictures or author notes alone; it
brings the whole source in, and a household that has edited a recipe heavily
reviews those edits as changes each time.

**Obligations:**

1. Plan node i-reimport (#462), after r-author-notes (#395), adds the
   re-import action and the proposal it stores.
2. i-reimport stores a second source snapshot for the recipe and a test shows
   the first snapshot's content is unchanged and, after the proposal is
   accepted, both are attached to the recipe.
3. i-reimport runs the rule-based path before any LLM call, with a test that
   the LLM is not called when the rules reach the confidence threshold.
4. Tests show a re-import supersedes a pending agent proposal for the same
   recipe, and that a failed fetch stores no proposal and shows its message.
5. A test re-imports an unchanged source into an unedited recipe and shows the
   change summary reports no differences. A second test edits a field first
   and shows the summary reports the edit as reverted.
6. A test accepts a re-import proposal and shows the recipe's journal, made-it
   entries and shares are unchanged and the previous version restores from
   history.
7. The re-import proposal carries picture candidates as an ingest draft does,
   and the editor review renders the shared picker for it, defaulting to the
   current picture. Tests show that saving with the default leaves the picture
   as it was, that choosing a candidate replaces it and deletes the previous
   object, that "No picture" removes it, and that a discarded or superseded
   re-import leaves no `drafts/<proposal_id>/` objects.
8. i-reimport adds the "Re-import from source" POST button at the bottom of the
   recipe page, hidden for a recipe with no source URL, with e2e coverage of both.
