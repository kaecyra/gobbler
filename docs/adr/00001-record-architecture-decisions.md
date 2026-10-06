# ADR-00001: Record architecture decisions

**Status:** Accepted

**Date:** 2026-10-06

## Context

Architectural reasoning is the perishable part of a project. The verdict
survives in the code; the rejected option and the argument that killed it do
not. So the same debate reopens later with none of the context that settled it,
and is re-argued by people who cannot tell whether it was already lost once.

A decision held only in conversation is lost at the next context boundary. A
decision held in an uncommitted file is one bad command from the same fate.

Options considered:

- **Leave decisions in issues and pull requests.** Rejected: they are organised
  by the work that prompted them, not by the decision, so finding "what did we
  decide about X" means knowing which pull request to look in. That is the
  archaeology this record exists to prevent.
- **One rolling decisions document.** Rejected: no stable citation. Entries get
  reordered and renumbered, and a link into the middle of a growing file is
  wrong as soon as anything is inserted above it.
- **Record nothing; trust the code.** Rejected: the code carries the verdict
  and never the alternatives, which is exactly the half that stops a settled
  question being reopened.

The convention is Michael Nygard's, and this file is its conventional first
entry: the decision to keep the decisions.

## Decision

Every architectural ruling is written to `docs/adr/` as a numbered, immutable
record before the work it authorises begins. One decision per file. Numbers are
zero-padded to five digits, never reused for a second decision, and never
changed once assigned — a number is the citation other records, rules and
conversations use, and only some of those live where a rename can reach them.

A gap in the sequence is therefore normal and is not a defect to repair: a
deleted record's number stays spent.

### Format

Copy [TEMPLATE.md](TEMPLATE.md) — it is the single definition of the format, so
this decision describes it rather than restating it. A file is named
`NNNNN-short-slug.md` and carries **Context**, **Decision** and
**Consequences**. Sections beyond those three are free-form.

The addition to Nygard's original is that **Consequences carries a numbered
obligation list**, not just prose. Consequences here double as the work-tracking
handoff into tickets, so they have to be checkable.

Three rules about content, and they are what separate a record from a note:

- **Write the reasoning, not just the verdict.** Without it the next reader
  relitigates.
- **Record what was rejected and why.** That is what stops it returning.
- **Obligations must be checkable.** "Update the docs" is not an obligation.
  "Amend `docs/api.md`'s authentication table to add a Bearer row" is.

### Grounding

An ADR grounds its claims in architecture: repository structure, schema
objects, interfaces, a symbol that *is* the contract, or another ADR by number
and section name.

**A source file path is a smell. A line number is worse.** Not banned —
suspicious. Nothing manages these records: no test, no linter, no gate. A code
reference inside one rots on the next refactor and nobody finds out. Justify
the reference in the text or replace it with the durable noun.

Rule files and `docs/` specifications are living documents; naming a real file
in one of those stays correct. This rule is about ADRs only.

### Immutability and supersession

An accepted ADR is not edited to change its meaning. When thinking moves on:

- **Superseded** — a new ADR replaces it. The old one gets `**Status:**
  Superseded by [ADR-NNNNN]` and keeps its text, so the reasoning survives.
- **Amended** — a later ADR narrows or qualifies part of it. The original gains
  a blockquote at the affected passage naming the amending ADR, and the
  amending ADR says what it changed.

Typos, broken links and formatting may be fixed in place. Meaning may not.

### Correction window

Nothing in this repository has reached production. Until the first production
deploy of gobbler, an accepted ADR found wrong may be corrected in place — same
number, same file, no record of the discarded verdict. From that deploy on,
immutability applies with no exception.

### When one is required

Anything expensive to reverse, or that a later reader would have to
reconstruct:

- Schema shape, data ownership, isolation boundaries
- Authorization and permission models
- What is in a release and what is deliberately deferred — a recorded deferral
  is a decision, not an omission
- Anything where a reasonable engineer would pick differently and both answers
  are defensible

Not required for: implementation detail inside an agreed design, library
choices with no architectural consequence, or anything a ticket already
captures.

### Process

The decision is made in conversation — one at a time, situation explained in
plain English, options argued honestly, the reasoning captured as it happens. A
written-first draft does not get the pushback that improves a decision.

**Verify the claims against the code before deciding.** Open questions are
usually written from memory and memory drifts.

**Record before moving on.** ADRs are committed as they are made, not at the
end of a pass. Where a decision's consequences are large, the ADR lands first
and the consequential edits follow in their own commit.

## Consequences

Settled questions stay settled, and the cost of reopening one is reading a file
rather than re-running the argument. Recording costs time at the moment a
decision is made, which is when it feels least necessary and is cheapest.

**Obligations:**

1. Add the new ADR's row to `docs/adr/README.md`'s index in the same commit
   that adds the record.
2. Keep `AGENTS.md` routing to `docs/adr/`, and stating that ADRs outrank every
   rule file.
3. Start a new record by copying [TEMPLATE.md](TEMPLATE.md), not an existing
   ADR. When the format changes, amend `TEMPLATE.md` rather than this file.
