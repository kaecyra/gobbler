# ADR-00049: Pending import drafts are listed on the import page and counted in the menu

**Status:** Accepted

**Date:** 2026-11-09

## Context

A pending ingestion draft, a re-import draft (ADR-00042) included, waits in the
proposals table (ADR-00035) under a token, and the review screen at
`/ingest/{token}` is the only page that shows it. No web page lists the
household's drafts. A draft is found only by the link that made it: the redirect
after the import form, or the review path an agent hands back over MCP
(ADR-00034). An agent importing a batch (ADR-00048) can leave up to 20 drafts of
new recipes, plus re-imports, and a person has no way to find them or work
through them. `list_recipe_drafts` already lists them for an agent, oldest
first, through the recipe service.

Options considered:

- **List the drafts on the import page only.** Rejected by the owner as not
  enough on its own: a person sees drafts waiting only when they go looking, and
  an agent's batch is made while nobody is looking.
- **List them on the import page, and count them on the menu's import entry.**
  Chosen. The count is on every signed-in page, so drafts waiting are noticed,
  and the list is where they are worked through.
- **Let a person tick several drafts and confirm them together.** Rejected.
  Each draft carries picture choices and low-confidence marks that a person
  must look at. A bulk confirm would be the one path that saves a recipe nobody
  read, which the review boundary of ADR-00006 and ADR-00034 exists to prevent.

## Decision

1. The import page shows a "Waiting for review" section above the import forms
   whenever at least one draft is pending in the household, and none
   otherwise. Each row gives the draft's name, its source (the host of its
   address, or "Pasted text" or "Photo" when it has none), how long ago it was
   made, a mark when an agent revised it (the ADR-00048 reason is present), and
   "Re-import of" the recipe's name for a re-import. Each row links to the
   draft's review page. The rows are every pending draft, oldest first, in
   `list_recipe_drafts`'s order, read through the same recipe-service list.
   There is no paging: the 20-draft bound plus at most one re-import per recipe
   keeps the list short.
2. After a person confirms or discards a draft, they land on the review page
   of the oldest draft still pending, with a page-level message saying what
   just happened ("Saved" and the recipe's name, linking to it, or "Discarded"
   and the draft's name) and how many drafts are still waiting. With none
   waiting, the destination is the one before this decision: the saved recipe
   after a confirm, the recipe after a discarded re-import, and the import page
   after any other discard, which shows the message too.
3. The redirect names the draft that ended by its token in a query parameter,
   never by free text. The page reads the name back from that draft in the
   household, accepted or rejected only, and shows nothing for a token that
   names no such draft.
4. The import entry in the main menu, in the header and in the phone's tab bar,
   shows the household's count of pending drafts, re-imports included, as a
   small badge while it is above zero. The count is one `COUNT` query scoped to
   the signed-in member's household, made only when a page renders the menu for
   a member.

## Consequences

This amends ADR-00035's rule that no read path outside proposal review and
display reads the proposals table: the menu's count reads the household's
pending drafts on every signed-in page, and the message after a confirm or
discard reads the name of the draft just ended. Both are reads of drafts
awaiting or just leaving review, made so that they get reviewed.

A person who confirms one draft while others wait lands on the next draft, not
on the recipe they saved; the message links to the recipe.

> Amended 2026-11-09: the message after a confirm or discard no longer says how
> many drafts are still waiting. It says what was saved or discarded, and the
> menu's badge carries the count.

> Amended 2026-11-10 (#586): the rows carry no agent-revised mark. A
> non-empty reason never meant an agent revised a draft (a re-import carried
> one from creation), and the review screen's LLM marks already show what an
> agent changed. Obligation 1's "the agent-revised mark" is withdrawn.

**Obligations:**

1. Tests show the import page's list carries the name, source, age, the
   agent-revised mark and the re-import mark, oldest first, and that another
   household's drafts are never listed or counted.
2. Tests show a confirm and a discard redirect to the oldest pending draft with
   the message, and to the earlier destination when none is waiting.
3. Tests show the menu's badge with the count, and no badge at zero.
4. E2e covers the import page's list, a confirm landing on the next draft, and
   the badge.
5. `docs/adr/README.md` gains this record's index row, and `docs/operations.md`
   and `docs/ui/design.md` describe the list, the move to the next draft and
   the badge.
