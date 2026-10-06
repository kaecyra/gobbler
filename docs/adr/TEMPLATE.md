# ADR-NNNNN: One-line statement of what was decided

**Status:** Proposed | Accepted | Superseded by [ADR-NNNNN](NNNNN-slug.md) | Amended

**Date:** YYYY-MM-DD

## Context

What is true, what is broken, and what constrains the answer.

Ground claims in the durable thing — repository structure, schema objects,
interfaces, a symbol that *is* the contract, or another ADR by number and
section name. A source file path is a smell and a line number is worse: this
record is immutable and nothing keeps its citations honest, so a code reference
inside it rots silently on the next refactor. Evidence at a specific location
belongs in the issue or pull request that prompted this ADR.

State the options that were considered and what killed each rejected one. An
option presented only to be knocked down is not a record of a decision — it is
a decision made for the reader while pretending otherwise.

## Decision

The ruling, stated so an implementation can be checked against it.

## Consequences

What is now committed to. Where a decision amends or narrows an earlier one,
say which and how — never leave two records disagreeing.

Reasoning appears once. A point made in Context is not restated here.

**Obligations:**

1. Specific and checkable. "Amend `docs/api.md`'s authentication table to add a
   Bearer row" is an obligation. "Update the docs" is not.

---

<!--
Delete this comment block when using the template.

Filename: NNNNN-short-slug.md — five digits, zero-padded, never reused.
Add a row to README.md's index in the same commit.

Sections beyond the three above are free-form: schemas, rules, tables,
boundaries — whatever the decision needs.

An accepted ADR is not edited to change its meaning. Supersede it with a new
ADR, or amend it with a note at the affected passage. Typos and broken links
may be fixed in place; meaning may not. The one window is correction in place
before the first production deploy — see README.md.

Full convention: 00001-record-architecture-decisions.md
-->
