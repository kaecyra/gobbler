# ADR-00044: An author note's length is flagged on review, never refused

**Status:** Accepted

**Date:** 2026-11-06

## Context

ADR-00041 made recipe validation refuse an author note over 400 characters or
over one paragraph, for every writer: ingest, the editor and MCP. Ingest keeps
an over-long candidate whole and flags it at low confidence, so the person
trims it on the review screen.

The first real import to hit the limit was a bread recipe whose notes block
held two single-paragraph tips of 700 and 550 characters. Confirm refused the
draft, and the refusal showed only inline on the two note rows, far below the
save button. The owner saw nothing happen. Trimming a long but good tip to fit
an arbitrary count is work the owner does not want; a long tip is still the
author's tip.

ADR-00041 rejected "guidance only" because the LLM fallback and agent edits
over MCP could write anything and the rule would decay. Both of those writers
already land on the review screen before anything changes a recipe
(ADR-00006, ADR-00034), so a person sees the note and its flag there. The
length rule's job was to keep notes short enough to read beside the steps;
the low-confidence flag ingest already puts on a long note does that without
refusing the save.

Options considered:

- **Raise the limit** (to 1,000 characters, say). Rejected: the next long tip
  refuses again, and any number is as arbitrary as 400.
- **Keep the limit and make the refusal visible.** Rejected by the owner: the
  person still has to cut a good tip to save the recipe.
- **A new warning in the editor above the guideline.** Rejected by the owner:
  the existing low-confidence flag on review is enough.
- **No length limit; keep the existing flag, never block.** Chosen.

The one-paragraph rule is not part of this decision. A note is still one
paragraph; ingest already splits a notes block at paragraph breaks, so a
multi-paragraph note only comes from a person or an agent typing one.

## Decision

- Recipe validation no longer refuses an author note for its length. The
  one-paragraph rule from ADR-00041 stands.
- 400 characters stays as the guideline. Ingest still flags a candidate over
  it at low confidence, so the review screen counts it among the items to
  look at. The flag never blocks a save.
- No new warning is added to the editor; MCP and the editor accept a long
  note.

## Consequences

Amends ADR-00041's Decision: the 400-character refusal is removed, and the
low-confidence flag remains; the paragraph rule and everything else in ADR-00041
stand.

Obligations:

1. Recipe validation stops refusing a note on length, and a unit test proves a
   note of 1,000 characters on one paragraph validates.
2. An e2e test confirms a draft carrying a 700-character note saves from the
   review screen, with the note still flagged before the save.
3. Ingest keeps flagging a note over 400 characters at low confidence; its
   existing test still passes unchanged.
4. ADR-00041's status line and its row in `docs/adr/README.md` say it is
   amended by this ADR.
