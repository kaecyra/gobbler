# ADR-00037: A note is a household scratchpad: its body is edited in place and it can be deleted

**Status:** Accepted

**Date:** 2026-11-05

## Context

ADR-00004 gives a recipe many free-text notes, any of which may be pinned as
a tip shown on the recipe. The store could only add a note and pin
it. A typo stayed forever, and a tip that proved wrong could be unpinned but
not removed. The owner asked for notes to be editable and deletable.

The question is whether a note is history. Revisions and source snapshots are
(ADR-00004, ADR-00015): they record what a recipe was and where it came from,
and nothing can rebuild them once lost. A note records what someone in the
household thinks now about cooking the recipe. An old wording of it has no
reader, and the made-it log already records when the recipe was made and how
it went.

Options considered:

- **Edit only, no delete.** Fixes typos, but leaves a wrong or obsolete note
  in the list, where the only remedy is to overwrite it with nothing useful.
  The `notes.body` column refuses an empty string, so a note cannot be emptied
  either.
- **Soft delete, with a tombstone column on `notes`.** Keeps a deleted note
  recoverable. Every read of notes (the journal, the tips, search) then has to
  filter on the column, and a missed filter shows a deleted note. That is a
  new class of bug, paid for a recovery nobody has asked for.
- **Neither: notes stay append-only.** Treats a note as history, which is the
  reading this record rejects. A correction then becomes a second note that
  contradicts the first.
- **Edit in place, and hard delete after a confirm step.** Chosen.

## Decision

- A note is a household scratchpad, not history. Any member of the household
  that owns the recipe may change its body or delete it.
- An edit replaces `notes.body` with a non-key `UPDATE`, which ADR-00015
  allows. The note keeps its id, author, creation time and pin. No earlier
  body is kept.
- A body that is empty after trimming whitespace is refused, as when a note is
  added.
- A delete removes the `notes` row. The interface asks for confirmation first,
  and the confirmation works without scripts.
- Made-it entries, revisions and source snapshots stay as ADR-00004 and
  ADR-00015 rule. Editing or deleting a note writes no revision.

## Consequences

Recipe search indexes note bodies, so an edit or a delete changes what a
search finds. A deleted pinned note stops being a tip.

**Obligations:**

1. The `store` package offers `UpdateNoteBody` and `DeleteNote`. Both are
   scoped to the household and recipe of the note, and return `ErrNotFound`
   for a note of another recipe or household.
2. `UpdateNoteBody` returns `ErrInvalid` for an empty or whitespace-only body,
   and leaves the stored body unchanged.
3. Store tests show that an edit keeps the note's id, author, creation time and
   pin, and that neither operation writes a revision.
4. The journal offers Edit and Delete on each note. Delete shows a confirmation
   in the note's place before anything is removed, and an end-to-end test
   covers each route without scripts.
5. No migration adds a deleted-at or tombstone column to `notes`.
