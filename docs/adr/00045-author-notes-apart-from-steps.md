# ADR-00045: Author notes sit apart from the step chain, with no connector lines

**Status:** Accepted

**Date:** 2026-11-06

## Context

ADR-00041 put a recipe's author notes at the end of the step chain on the
recipe page: each note sits where a step number would, behind a bulb in a soft
wattle circle, joined to the next by a dotted line, and the chain runs on from
the last step into the first note.

On 2026-11-06 the owner asked for the notes to be disconnected from the steps:
by spacing and by removing the connector line, with the alignment left as it
is.

Options considered:

- **Remove only the line from the last step into the notes**, keeping the
  notes chained to each other. Offered and declined by the owner.
- **A section of its own with a heading.** Already rejected in ADR-00041; the
  owner kept the notes' place and alignment.
- **Remove every connector line and separate the notes from the steps by
  space, keeping their alignment.** Chosen by the owner.

## Decision

- On the recipe page, no connector line joins the last step to the first note,
  and none joins one note to the next.
- The notes are separated from the steps by extra vertical space.
- Everything else in ADR-00041's description stands: each note's bulb still
  sits in the step-number column, with the same title, text and "From the
  author" treatment, after the last step. Where the mockup ADR-00041 adopted
  draws dotted connectors to or between notes, this decision overrides it.
- Cook mode is unchanged.

## Consequences

Amends ADR-00041's Decision on how notes show on the recipe page: the dotted
line between notes, and the chain running from the last step into them, are
removed. Their position and alignment stand.

Obligations:

1. The recipe page draws no connector between the last step and the first
   note, or between notes, and an e2e test proves it on a recipe with at least
   two notes.
2. The gap between the last step and the first note is larger than the gap
   between two steps, and the e2e test asserts it.
3. `docs/ui/design.md`'s description of author notes on the recipe page no
   longer says they continue the step chain or are joined by a dotted line.
4. ADR-00041's status line and its row in `docs/adr/README.md` name this ADR
   among its amendments.
