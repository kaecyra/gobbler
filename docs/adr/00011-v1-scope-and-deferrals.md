# ADR-00011: Scope and deferrals

**Status:** Accepted

**Date:** 2026-10-06

## Context

The planning conversation produced more ideas than a first build needs. Writing
down what is deliberately out keeps it from creeping back in by accident, and
records why, so the decision can be revisited on purpose.

## Decision

### In scope

Ingestion from URL, pasted text and photo (ADR-00006, ADR-00007); the recipe
model with components, step sequencing, revisions, clone, notes, ratings and a
made-it log (ADR-00004); scaling and unit display modes (ADR-00005); the
ingredient catalog (ADR-00008); shopping lists with pantry, live view, text
export and Todoist push (ADR-00009); full-text search and tags; a cook mode
that steps through the recipe one step at a time, keeps the screen awake and
runs step timers; and nightly backups (ADR-00010).

The work is delivered in waves: foundation, domain core, rule-based ingestion,
recipe UI, shopping, LLM and photo ingestion, cooking, and operations and
integrations. The plan graph holds the detail.

### Deferred, planned later

- **Meal planning** (a weekly plan that feeds the shopping list).
- **Invitations and multiple users.** The app records who made each write
  (ADR-00003) so this can be added without a data migration.

### Out, until decided otherwise

- **Images.** No recipe photos, downloaded or uploaded.
- **Computed nutrition.** Calories are entered or scraped, not computed from
  ingredients.
- **Timeline (Gantt) view** of the step graph. "Meanwhile" guidance in the
  linear view and cook mode covers the need.
- **Installable / offline app (PWA).** A responsive web app is enough.
- **Apple Reminders** (ADR-00009).

## Consequences

Anything in "Out" needs a new ADR to bring it in.

**Obligations:**

1. The plan graph contains no node for an item listed under "Out".
2. Meal planning and invitations appear in the plan graph as a later wave,
   after the operations wave.
