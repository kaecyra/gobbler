# Open decisions: the recipe author's notes

Owner request, issue #395: keep the author's own notes from the source page (tips, substitutions,
storage) instead of dropping them at ingest. Worked through with `/overseer:decide`; all decisions
are below. Ruled in [ADR-00041](../adr/00041-author-notes.md), which carries the obligations; the
work is plan node r-author-notes (#395).

## Verified facts

- A revision stores the whole recipe as one JSON document (`revisions.document`,
  `internal/db/migrations/00029_a-mcp-recipe-edit.sql:15`), so a field added to `recipe.Recipe`
  (`internal/recipe/model.go:141`) is kept in history with no extra work. The current recipe is
  stored relationally (`recipe_tags`, `steps`, `components`), so author notes need a table of their
  own.
- The plugin extractor reads WPRM and Tasty Recipes markup (`internal/ingest/extract/plugins.go`) but
  no notes. `real_recipetineats_graph.html:476` already carries a `.wprm-recipe-notes-container`.
- schema.org `Recipe` has no notes property. `HowToTip` exists for steps, but no code reads it and no
  fixture under `internal/ingest/extract/testdata/` contains one. WPRM has no per-step notes class.
- The recipe page already shows pinned journal notes as "Tip" callouts above the steps, labelled
  "From your notes" (`internal/web/recipeview/view.templ:275-281`, `model.go:55`). The journal's own
  section is headed "Notes" (`internal/web/journal/journal.templ:130`).

## D1 — Author notes are an ordered list on the recipe, recipe-level only

**Decided.** A recipe carries an ordered list of author notes, each with an optional title and a
required text (`[]AuthorNote{Title, Text}` on `recipe.Recipe`). They belong to the recipe and are
kept in its revisions like any other field. A note attaches to the recipe as a whole, never to a
step; the schema carries no step link.

**Why.** It keeps the source's structure (the bold lead "Honey:" becomes the title, the same shape
step titles already parse from) and keeps the author's words apart from the household's.

Rejected:
- **Fold into Description.** No new table, but the title/text structure is lost and the description
  turns into a wall of text on the card and the header.
- **Store as journal notes.** They would reuse the Tip callouts for free, but journal notes are the
  household's own timestamped words (ADR-00004 §Notes); mixing in the author's text loses whose
  words they are, and they would not travel with the recipe's revisions.
- **Per-step notes** (an optional step link on each note, or a `Note` field on `Step`). Raised by the
  owner. No source we read supplies them (no `HowToTip` in code or fixtures, no WPRM per-step notes
  class), so they would be typed by the household, which is journal territory. Reserving an unused
  nullable `step_id` was also offered and declined: recipe level only.

**Obligations:**

1. The ADR for this record amends ADR-00004's data model with the author-notes list.
2. r-author-notes (#395) adds `AuthorNote` to `recipe.Recipe` and a migration for its table, with the
   migration number assigned by the dispatcher.

## D2 — Author notes end the step chain, each credited quietly to the author

**Decided.** On the recipe page, author notes continue the step chain after the last step, not in a
section of their own. Each note sits where a step number would, behind a small bulb in a soft wattle
circle, and the notes are joined by a dotted line, so they read as an end to the chain rather than more
steps. A note shows its title in bold, when it has one, then its text, a little smaller and lighter
than step text. Each note ends with a small grey "From the author", the same treatment as the journal
Tip's "From your notes". Cook mode shows the notes behind a button, since it shows one step at a time.

Notes are concise cooking tips or information: about one paragraph each at most. The long material
recipe sites carry (life-story preamble, nutrition dumps, publication history, meal plans) is not
author notes and does not belong in gobbler (D3 enforces this).

The visual target is the owner-approved mockup, variant A, drawn on `docs/ui/mockup.html`'s recipe
screen.

**Why.** Most source sites put their notes after the method, and keeping them in the chain puts them
where the cook's eye already is when the method ends, without pushing the steps down. A note needed
before step 1 (the honey warning) can still be pinned as a Tip from the journal, which shows above the
steps. The credit on each note keeps the author's words apart from the household's without a heading
competing with the steps.

Rejected:
- **A section of its own under the steps**, with a heading. First proposed here; the owner moved the
  notes into the step chain instead.
- **One credit line under the last note** ("Tips from the author, Serious Eats"; mockup variant B).
  Quieter when a recipe has many notes, but a note read on its own carries no sign of whose it is. The
  owner chose a credit on each note.
- **Callouts beside the journal Tips, above the steps**, labelled "From the recipe". Seen before
  cooking, but a recipe with many notes puts a wall of callouts between the ingredients and the steps.
- **A collapsed section above the steps.** Free until opened, but collapsed content is often never
  opened, so the warning it was moved up for is missed anyway.

The owner's concern behind the conciseness rule is real in the fixtures: the WPRM notes block in
`real_recipetineats_graph.html:476` holds short tips (76–237 characters) beside a 368-character
nutrition table, an "Originally published in March 2017…" line and a "Meal Ideas" section.

**Obligations:**

1. The approved mockup (variant A) is added to `docs/ui/mockup.html`'s recipe screen, after step 6.
2. r-author-notes renders author notes at the end of the step chain on the recipe page as that mockup
   draws them, checked side by side against it, and behind an "Author's Notes" button in cook mode.

## D3 — A note is one paragraph of at most 400 characters, enforced by the model and flagged at ingest

**Decided.** `recipe.Recipe` validation refuses an author note whose text holds more than one
paragraph (a blank line) or more than 400 characters, so the limit holds for every writer: ingest,
the editor and MCP. Ingest makes each paragraph of the source's notes block a candidate note. A
candidate over the limit, or one that reads as nutrition facts, publication history or a meal plan,
arrives on the review screen at low confidence, where the person trims or deletes it. Ingest drops
nothing on its own.

**Why.** It keeps the owner's "concise cooking tips only" rule true after save, while leaving the
judgement about a borderline note to the person on the review screen (ADR-00006: low confidence is
data, not an error).

Rejected:
- **Ingest drops paragraphs that fail the rule.** The review is cleaner, but junk detection is
  heuristic, and a good long tip, or one that mentions calories, would vanish with no trace.
- **Guidance only, no enforced limit.** Cheapest, but the LLM fallback and MCP edits could write
  anything, and the rule would decay.

**Obligations:**

1. r-author-notes adds the one-paragraph, 400-character rule to `recipe.Recipe` validation, with a
   test at the boundary.
2. r-author-notes flags candidates over the limit, and those matching the junk patterns, at low
   confidence on the review screen, with a fixture test from `real_recipetineats_graph.html`.

## D4 — The on-screen name is "Author's Notes"

**Decided.** Where the notes need a name (the cook-mode button, the review screen's section, the
editor's section), it is "Author's Notes". On the recipe page itself each note is credited "From the
author" (D2), so no heading is needed there.

**Why.** It says whose words they are, and the qualifier keeps it apart from the journal's "Notes".

Rejected:
- **"From the recipe"** (#395's proposal). Pairs with "From your notes", but reads oddly as a name
  when everything on the page is from the recipe.
- **"Notes from <author>"**. Concrete, but `Author` is often empty or a site name, so it needs a
  fallback and two variants to test.
- **"Tips"**. Collides with the household's Tip callouts on the same page.

**Obligations:**

1. r-author-notes labels the cook-mode button and the review and editor sections "Author's Notes".

## D5 — Review shows author notes as editable rows

**Decided.** The review screen and the editor show each author note as an editable row with a title
and a text, which can be removed or reordered. A low-confidence note (D3) is flagged as a
low-confidence ingredient line is.

**Why.** It follows ADR-00006 and the existing review rows; no alternative was argued.

**Obligations:**

1. r-author-notes adds author-note rows to the review screen and the editor, with e2e coverage.
