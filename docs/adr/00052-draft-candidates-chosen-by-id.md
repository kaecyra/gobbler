# ADR-00052: A draft's picture candidates are chosen by an ID of their own

**Status:** Accepted

**Date:** 2026-11-10

## Context

ADR-00039, "Choosing", has the review screen offer a draft's picture
candidates, and the person picks one before confirming. The choice was built
as the candidate's position in the row: the picker's radios posted a number,
the draft document stored it, and confirming promoted whichever candidate sat
at that position. Fetching a draft's candidates replaces the whole set, so a
choice posted from a page drawn before a fetch, or stored on the draft before
one, named whatever picture the fetch put at that position.

ADR-00051 met the same flaw in the edit page's offered pictures and gave each
one an opaque ID. The owner, on that review: "pictures should have IDs lol.
thats wild. positional? crazy. even transient ids." The import review should
work the same way.

Options considered for the ID:

- **The `draft_candidates` row key.** Rejected: the table's `INTEGER PRIMARY
  KEY` is a SQLite rowid, which hands the highest deleted value out again, so
  a fetch that deletes the last rows and inserts new ones can reuse an ID.
  Making it `AUTOINCREMENT` would fix that, but the key would still be a
  guessable counter shared by every household, and it would differ from the
  edit page's offers.
- **A random token, as ADR-00051's offers have.** Chosen. Each candidate draws
  one when it is fetched. The same shape is accepted in both places, and the
  object key can carry it.

## Decision

- Each row of `draft_candidates` carries an `offer`: a random token, unique,
  drawn when the candidate is fetched and never reused. `position` still
  orders the picker.
- The picker's radio value, the candidate's address
  (`/pictures/drafts/{proposal}/{offer}`), the choice stored in the draft
  document, and the promotion on confirm all name the candidate by its
  `offer`. Nothing resolves a choice by position.
- A posted choice whose ID is not one of the draft's current candidates is a
  bad request. A stored choice whose candidate a later fetch replaced stops
  the confirm, and the review screen asks for a choice again.
- New candidate objects are kept under `drafts/<proposal_id>/<offer>.webp`, so
  a fetch never writes over an object a page already shows.
- The draft document's `picture` becomes a string: the candidate's `offer`,
  `"none"` for "No picture", or `"keep"` for a re-import's "Keep current
  picture" (ADR-00042). The migration that adds `offer` rewrites the pending
  drafts' stored positions to their candidates' IDs, so a choice already made
  survives it.

## Consequences

This amends ADR-00039: in "Choosing", a candidate is chosen by its ID; in
"Storing", a draft candidate's key is `drafts/<proposal_id>/<offer>.webp`
instead of `drafts/<proposal_id>/<n>.webp`. Candidates fetched before the
migration keep the keys their rows name until the draft ends.

**Obligations:**

1. ADR-00039 carries an "Amended by ADR-00052" note at "Choosing" and at the
   `drafts/` key in "Storing", its status line names ADR-00052, and its row in
   `docs/adr/README.md` lists ADR-00052 among its amendments.
2. A test in `internal/ingest` shows that after the candidates are fetched
   again, the earlier choice is refused by `ChoosePicture` and stops `Confirm`,
   and that a choice of a new candidate confirms.
3. A test in `internal/picture` shows a second fetch drawing IDs that the
   first did not, and `Promote` refusing an ID that is not a candidate,
   including one a concurrent fetch replaced.
4. Tests in `internal/web/ingest` and the e2e picture spec show a position, or
   a well-formed ID the draft does not have, refused with 400.
