# ADR-00041: A recipe keeps its author's notes as a short list that ends the step chain

**Status:** Amended by [ADR-00044](00044-author-note-length-flagged.md)

**Date:** 2026-11-06

## Context

The owner wants the author's own notes from a source page (tips,
substitutions, storage) kept at ingest instead of dropped. ADR-00004's
"Notes, history and provenance" section defines one kind of note: free text,
timestamped, authored by the household. Nothing holds words that belong to the
recipe's author. The decisions below were made one at a time with the owner;
the working record, with the evidence checked against the code, is
`docs/proposals/author-notes.md`.

A revision stores the whole recipe as one document, so a field added to the
recipe model is kept in history without further work. The current recipe is
stored relationally, so the list needs a table of its own. The plugin
extractor reads WPRM and Tasty Recipes markup but no notes, and the WPRM notes
block in the test fixtures holds short tips beside nutrition tables,
publication history and meal ideas. schema.org `Recipe` has no notes property,
and no source we read supplies per-step notes.

Options considered for the model:

- **Fold the notes into the description.** Rejected: the title and text
  structure is lost, and the description turns into a wall of text on the card
  and the header.
- **Store them as journal notes.** Rejected: journal notes are the household's
  timestamped words, so mixing in the author's text loses whose words they are,
  and they would not travel with the recipe's revisions.
- **Per-step notes**, as an optional step link on each note or a field on the
  step. Rejected: no source supplies them, so they would be typed by the
  household, which is journal territory. An unused nullable step link was also
  offered and declined.
- **An ordered list on the recipe, recipe level only.** Chosen.

Options considered for the limit on a note:

- **Ingest drops paragraphs that fail the rule.** Rejected: junk detection is
  heuristic, and a good long tip, or one that mentions calories, would vanish
  with no trace.
- **Guidance only, with no enforced limit.** Rejected: the LLM fallback and
  agent edits over MCP could write anything, and the rule would decay.
- **Validation on the recipe, with ingest flagging candidates.** Chosen.

Options considered for where notes show on the recipe page:

- **A section of its own under the steps, with a heading.** Rejected by the
  owner in favour of the step chain.
- **Callouts beside the journal Tips, above the steps.** Rejected: a recipe
  with many notes puts a wall of callouts between the ingredients and the
  steps.
- **A collapsed section above the steps.** Rejected: collapsed content is often
  never opened, so the warning it was moved up for is missed anyway.
- **One credit line under the last note.** Rejected: a note read on its own
  carries no sign of whose it is.
- **Notes ending the step chain, each credited.** Chosen.

Options considered for the name:

- **"From the recipe".** Rejected: everything on the page is from the recipe.
- **"Notes from" the author's name.** Rejected: the author is often empty or a
  site name, so it needs a fallback and two variants.
- **"Tips".** Rejected: it collides with the household's Tip callouts.
- **"Author's Notes".** Chosen.

## Decision

- A recipe carries an ordered list of author notes, each with an optional
  title and a required text. The list belongs to the recipe and is kept in its
  revisions like any other field. A note attaches to the recipe as a whole;
  the schema carries no step link.
- Recipe validation refuses a note whose text holds more than one paragraph
  or more than 400 characters, so the limit holds for every writer: ingest,
  the editor and MCP.
- Ingest makes each paragraph of the source's notes block a candidate note. A
  candidate over the limit, or one that reads as nutrition facts, publication
  history or a meal plan, arrives on the review screen at low confidence
  (ADR-00006). Ingest drops nothing on its own.
- The review screen and the editor show each note as an editable row with a
  title and a text, which can be removed or reordered. A low-confidence note is
  flagged as a low-confidence ingredient line is.
- On the recipe page the notes continue the step chain after the last step.
  Each sits where a step number would, behind a small bulb in a soft wattle
  circle, joined to the next by a dotted line. A note shows its title in bold
  when it has one, then its text a little smaller and lighter than step text,
  and ends with a small grey "From the author", the treatment the journal Tip
  gives "From your notes". A note needed before step 1 can still be pinned as
  a Tip, which shows above the steps.
- Cook mode shows one step at a time, so it reaches the notes through a button.
- Where a name is needed (the cook-mode button, the review screen's section,
  the editor's section), it is "Author's Notes". The recipe page needs no
  heading, because each note carries its credit.
- The visual target is variant A of the recipe screen in
  `docs/ui/mockup.html`, approved by the owner.

## Consequences

This amends ADR-00004's data model: "Notes, history and provenance" gains the
author-notes list beside the household's notes, and the schema obligation that
lists the tables gains one for them. ADR-00004 is not edited. Notes stay short
tips; the long material recipe sites carry beside them does not enter
gobbler.

**Obligations:**

1. Plan node r-author-notes (#395) adds the author-notes list to the recipe
   model and a migration for its table, with the migration number assigned by
   the dispatcher at dispatch.
2. r-author-notes adds the one-paragraph, 400-character rule to recipe
   validation, with a test at the boundary.
3. r-author-notes flags candidates over the limit, and those matching the
   nutrition, publication-history and meal-plan patterns, at low confidence
   on the review screen, with a fixture test from the RecipeTin Eats page
   (`real_recipetineats_graph.html`).
4. r-author-notes adds author-note rows to the review screen and the editor,
   with e2e coverage.
5. r-author-notes renders the notes at the end of the step chain on the recipe
   page as the mockup draws them, checked side by side against variant A in
   `docs/ui/mockup.html` (lessons.md, "An approved mockup is the visual
   target").
6. r-author-notes adds an "Author's Notes" button to cook mode, and labels the
   review screen's and the editor's sections "Author's Notes".
7. The pull request that adds this ADR carries variant A in
   `docs/ui/mockup.html`'s recipe screen, after step 6, and a paragraph on
   author notes in `docs/ui/design.md`.
