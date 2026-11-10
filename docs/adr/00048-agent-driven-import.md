# ADR-00048: Agents preview imports, revise held drafts against a household house style, and leave one review per recipe

**Status:** Accepted

**Date:** 2026-11-09

## Context

The owner wants an agent to import the rest of their bookmarked recipes with
little attention from them. The agent reads how the owner reshapes an import
(components, ingredient wording, step splits, notes), then imports new URLs and
reshapes each draft the same way before the owner sees it.

Over MCP today an agent can import a URL and get a held draft (ADR-00034), and
read a saved recipe. It cannot:

- see what an import produced before the owner edited it, so it has nothing
  to compare a finished recipe with;
- change a held draft, so its reshaping waits until the draft is confirmed,
  and then needs a second review as an edit proposal (ADR-00035);
- read a recipe's author notes, times, equipment, meal type or cuisine;
- tell that a URL is already a recipe or a pending draft.

The ingestion pipeline (ADR-00006) already separates drafting from holding.
Drafting fetches, extracts, applies the rules and the LLM fallback, and
returns a draft, writing nothing. Holding stores the draft as a proposal with
its source snapshot (ADR-00035, "Pending proposals"). The review screen
already replaces a held draft after a person's edit or an LLM retry. Every
saved import keeps the page it was read from as a source snapshot whose
content never changes (ADR-00004), and the same input and parser version give
the same draft (ADR-00006).

Options considered for showing an agent what an import would produce:

- **Hold a draft and discard it.** No new code. Rejected: it writes a
  snapshot and picture candidates, counts against the 20-draft bound and
  appears in the household's review list.
- **A separate parse path for previews.** Rejected by the owner: two
  implementations drift.
- **Run the drafting step and return its draft without holding it.** Chosen.
  Preview and import are one function, differing only in whether the caller
  holds the result.

Options considered for what a preview reads:

- **The URL only.** Rejected: a page may have changed since it was imported,
  and some sites refuse a fetch, so the comparison with a finished recipe would
  be against a different input.
- **The URL, or a saved recipe's original snapshot.** Chosen by the owner,
  because the snapshot already exists. Replaying it gives exactly the draft
  the person started from, so the difference from the saved recipe is the
  person's edits, plus any parser change since, which the parser versions
  reveal.

Options considered for applying an agent's reshaping:

- **A trusted credential that saves without review.** Rejected: it overturns
  the AGENTS.md boundary and ADR-00006 and ADR-00034, and the review screen is
  the only check between an agent and data that cannot be recreated.
- **An import that takes the reshaped recipe in the same call.** One call.
  Rejected: the agent must preview first to have something to reshape, so the
  page is fetched twice and the reshaped recipe can be paired with a snapshot
  of a different page.
- **Import, then revise the held draft.** Chosen by the owner. One fetch, the
  snapshot and the reshaped draft stay together, and the person reviews once
  on the ingest review screen with its confidence flags and picture picker.

Options considered for where the household's style rules live:

- **In the agent's own memory.** Rejected by the owner: only that agent can
  use it, and the owner cannot read or correct it in gobbler.
- **A household setting, edited on the household page and read over MCP.**
  Chosen by the owner. Every agent the household connects follows the same
  rules, and the owner corrects them where they live.

Options considered for who writes the house style:

- **Members and agents.** Rejected: an agent rewriting the rules that steer
  its own later edits, with no review, is the unreviewed agent write
  ADR-00034 refuses for recipes.
- **Members only, on the household page.** Chosen. An agent proposes rule
  changes to a person in conversation.

Options considered for the 20 pending drafts bound (ADR-00035):

- **Exempt drafts an agent has revised.** Rejected by the owner: the bound is
  also what limits a looping agent or a leaked key.
- **Keep it.** Chosen. A batch stops at 20 and the agent reports that drafts
  are waiting for review.

## Decision

### Preview

- An MCP tool, `preview_import`, takes a `url` or a `recipe_id`, never both.
  It writes nothing: no proposal, no snapshot, no picture candidate, no
  credential write record. It is not counted against the 20-draft bound.
- Given a `url`, it runs the same drafting operation `import_recipe` runs,
  in the ingestion service under `internal/api/ingest` (ADR-00043). The
  pipeline gains one entry that drafts from page content and a source address;
  drafting from a URL becomes fetching and then calling that entry, so a
  preview and an import cannot differ after the fetch.
- Given a `recipe_id`, it drafts from the recipe's first HTML snapshot, the
  original (ADR-00042), through that same entry, with no fetch. A recipe with
  no HTML snapshot is refused with a message saying so.
- The rule-based path runs first and the LLM fallback runs only when the
  draft scores below the threshold, as for an import (ADR-00006). The result
  says whether the LLM was asked.
- The result is the draft as the review screen would receive it: every field
  with its confidence and LLM mark, components with lines and steps, the step
  graph, author notes, times, equipment, meal type, cuisine, the parse method,
  and the current parser version. For a `recipe_id` it also gives the parser
  version stored with the snapshot.

### Reading a saved recipe

- `get_recipe` adds the recipe's author notes, prep, cook and total times,
  equipment, meal type and cuisine, so a saved recipe can be compared with a
  preview field for field.

### Revising a held draft

- An MCP tool, `revise_recipe_draft`, takes a draft's token, the draft's
  revision as last read, a whole recipe in the shape `get_recipe_draft`
  returns, and a reason.
- It replaces the held draft through the replace operation the review screen
  uses. The source snapshot, source address and parser version are not
  changed by it. Any field, line or step whose value differs from the draft
  it replaces is marked as LLM-supplied, so the review screen marks it as it
  marks the fallback's values (ADR-00006).
- It is refused when the draft's revision has changed since the agent read
  it, so an agent never overwrites a person's edit on the review screen. The
  revision is derived from the held document's content; nothing is stored for
  it.
- The reason is stored on the proposal and shown on the review screen.
- It applies to any pending ingestion draft in the household, a re-import
  draft (ADR-00042) included. It never saves the draft.
- `get_recipe_draft` returns one pending draft in the preview's shape, with
  its revision and review path. `list_recipe_drafts` lists the household's
  pending drafts, a page at a time (ADR-00034, "Limits"), with each token,
  name, source address, creation time and whether an agent has revised it.

### Duplicates

- `preview_import` and `import_recipe` given a `url` report the household's
  recipes whose source address is the same, and its pending drafts whose
  source address is the same. Addresses are compared without their fragment.
  They report and do not refuse; the agent decides.

### House style

- A household has one house style: plain text, at most 16 KiB, empty by
  default, stored with the household.
- Any member edits it on the household page. Saving it changes nothing else.
- An MCP tool, `get_house_style`, returns it. No MCP tool writes it.
- The MCP server's instructions tell an agent to read the house style
  before revising a draft or proposing a recipe edit.

## Consequences

This amends ADR-00035's rule that no read path outside proposal review and
display reads the proposals table: the duplicate check reads pending drafts'
source addresses. It is still a read of drafts awaiting review, made to keep a
second one from being held.

> Amended 2026-11-09: a preview by `recipe_id` also reads the document of the
> draft that held the recipe's first snapshot, for its source address. It reads
> one accepted draft and writes nothing.

> Amended 2026-11-09: the reason is no longer shown on the review screen; the
> owner found it noise beside the LLM marks, which already show what changed.
> It is still stored with the draft and returned over MCP by
> `get_recipe_draft`. Obligation 6's "the reason shows on the review screen" is
> withdrawn.

> Amended 2026-11-10 (#586): a proposal's reason is only ever an agent's own
> words, as ADR-00035 defines it. `import_recipe` and `create_recipe_draft`
> take an optional reason, stored with the draft; `revise_recipe_draft`
> replaces it. `list_recipe_drafts` no longer reports whether an agent revised
> a draft: the review screen's LLM marks show what an agent changed.

ADR-00034's rule that every recipe write from MCP waits for review holds:
`revise_recipe_draft` changes only a draft that is itself waiting for review.

**Obligations:**

1. A test drafts one page through `import_recipe` and through
   `preview_import` with a `url`, and shows the two drafts are equal.
2. A test shows `preview_import` writes no proposal, snapshot, picture object
   or credential write record, and succeeds when the household already holds
   20 pending drafts.
3. A test saves an import, then calls `preview_import` with its `recipe_id`
   with fetching disabled, and shows the draft equals the one first held, and
   that the snapshot's parser version is reported.
4. A test shows `preview_import` with a high-confidence page makes no provider
   call, and the result says the LLM was not asked.
5. `get_recipe` output carries author notes, the three times, equipment,
   meal type and cuisine, each covered by a test.
6. A test revises a held draft and shows the snapshot is unchanged, each
   changed field, line and step is marked LLM-supplied and an unchanged one
   is not, the reason shows on the review screen, and no recipe is created.
7. A test reads a draft, replaces it as the review screen does, then revises
   it with the old revision, and shows the revision is refused and the draft
   holds the person's edit.
8. Tests show `import_recipe` and `preview_import` report a saved recipe and
   a pending draft with the same source address, ignoring a fragment.
9. A household migration adds the house style. Tests show a member saves it
   on the household page, a value over 16 KiB is refused with a message,
   `get_house_style` returns it, and no MCP tool changes it. E2e covers the
   household page field.
10. `docs/operations.md`'s MCP section lists the new tools, and
    `docs/adr/README.md` gains this record's index row.
