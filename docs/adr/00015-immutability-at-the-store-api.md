# ADR-00015: History immutability is enforced by the store API, not the database

**Status:** Accepted

**Date:** 2026-10-12

## Context

ADR-00004 makes source snapshots immutable (obligation 4: no update path in the
store) and treats revisions as history. During review of the first `store`
implementation, each round asked how raw SQL outside the `store` API could
still alter those rows: `INSERT OR REPLACE`, `REPLACE`, `UPDATE OR REPLACE`,
re-keying through `rowid`. Each answer was another SQLite trigger. Nothing in
the codebase issues any of those statements, and the list of such statements
has no end.

Options considered:

- **Triggers for every write path that could bypass the API.** Each new path
  needs a new trigger, a test and a review round. The schema fills with
  defensive DDL that guards against code that does not exist, and a
  `DROP TRIGGER` defeats it anyway.
- **`PRAGMA recursive_triggers` plus delete triggers.** Closes the REPLACE
  family in one setting, but still guards against code that does not exist,
  and keeps the defensive triggers it depends on.
- **Enforce at the store API.** The `store` package exposes no operation that
  updates a snapshot, deletes a revision, or replaces or re-keys a history row.
  SQL lives only in the owning package (ADR-00014), so that package is the
  only place where such SQL could appear, and code review covers it. Chosen.

## Decision

- Immutability of source snapshots and revisions is a property of the `store`
  API. The schema carries no triggers to enforce it.
- The `store` package issues no `REPLACE`, upsert or key-changing `UPDATE`
  against `source_snapshots`, `revisions`, `recipes`, `notes` or `made_it`.
- Tests prove the API offers no mutating path. Tests do not run raw SQL to
  attack the schema.

## Consequences

ADR-00004 obligation 4 is read as a store-level rule, which is what it says.
Code that bypasses `store` with raw SQL is a defect for review to catch, not for
the schema to refuse. Deleting a recipe still cascades to its history; that is
the one sanctioned way history disappears.

**Obligations:**

1. The `goose` migrations define no `CREATE TRIGGER` on `source_snapshots`,
   `revisions`, `recipes`, `notes` or `made_it` for immutability.
2. No `store` test executes `REPLACE`, `INSERT OR REPLACE`, `UPDATE OR REPLACE`
   or a `rowid` update against those tables.
