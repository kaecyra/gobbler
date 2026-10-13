# ADR-00019: Equipment carries a hand-set `sized` flag that drives the scaled view's larger-pan hint

**Status:** Accepted

**Date:** 2026-10-13

## Context

[ADR-00005](00005-quantities-and-units.md), section Scaling, says a scaled view
shows a hint on "equipment with a size" ("may need a larger pan"). Nothing
records which equipment has a size. The catalog's `equipment` table holds a
name, its aliases and a `reviewed` flag, and an ingested equipment line is
matched to that row by name.

Two ways to know that a piece of equipment has a size were considered:

- **A flag on catalog equipment, set by hand during catalog review.** Pans,
  tins, dishes and pots are flagged once, and every recipe that uses them gets
  the hint. It costs one decision per catalog row. Chosen.
- **A size parsed from the ingested equipment line** ("9-inch round pan",
  "20 cm tin"). It would carry an actual dimension and could later say how much
  larger. It was not chosen: recipe sites state sizes inconsistently or not at
  all, so the parse is guesswork, and a useful hint needs pan-area arithmetic
  nobody has asked for. Nothing in this decision prevents adding it on top of
  the flag later.

The owner chose the flag on 2026-10-13 (issue #85).

## Decision

- Catalog equipment gains a boolean `sized`, default false. It is a property of
  the equipment row, not of a recipe's use of it.
- `sized` is set and cleared by hand on the catalog review page, alongside
  `reviewed`. Ingestion never sets it.
- A scaled recipe view (any multiplier other than 1) shows the larger-pan hint
  on an equipment line exactly when that line's catalog equipment is `sized`.
  An unscaled view never shows it. Equipment that is not `sized` never shows it.
- Once the per-household catalog overlay exists
  ([ADR-00018](00018-households-as-tenants.md)), the flag lives on whichever
  equipment row the recipe resolves to, central or overlay, and promotion
  carries it with the row.

## Consequences

ADR-00005's "equipment with a size" now means equipment whose catalog row is
`sized`. Equipment is never scaled; this decision only governs the hint.

**Obligations:**

1. A `schema` migration adds `sized INTEGER NOT NULL DEFAULT 0 CHECK (sized IN
   (0, 1))` to catalog equipment.
2. The catalog review page lists equipment and lets whoever may review that
   row (ADR-00018) set and clear `sized`, covered by an e2e test.
3. The recipe view's scaled rendering shows the hint on `sized` equipment only,
   with tests for a sized and an unsized line, each scaled and unscaled.
