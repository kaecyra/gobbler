# ADR-00008: Canonical ingredient catalog: curated seed plus USDA FoodData Central

**Status:** Accepted

**Date:** 2026-10-06

## Context

Shopping lists merge ingredient lines by what is bought, not by how a recipe
words it. "Unsalted butter, melted" and "butter, softened" are one purchase.
Weight conversion (ADR-00005) needs a density per ingredient, and grouping by
store aisle needs an aisle per ingredient.

Options considered:

- **Free-text ingredient names, merged by string match.** Fails on plurals,
  synonyms and qualifiers. Rejected.
- **Hand-curated catalog only.** Good quality, but every new ingredient is
  manual work. Kept as the base, not the whole answer.
- **USDA FoodData Central only.** Broad coverage, but its names are scientific
  rather than kitchen language ("Butter, without salt"), and it has no aisle
  data. Kept as a supplement.
- **Curated seed, USDA import, and auto-creation on ingest.** Chosen.

## Decision

A **canonical ingredient** has a name, aliases (including plurals and common
synonyms), an aisle, an optional density (grams per millilitre), an optional
USDA FoodData Central ID, and a `reviewed` flag.

- **Seed:** a curated list of common ingredients with aliases, aisle and
  density, shipped as data and loaded by migration.
- **USDA import:** a command imports from FoodData Central to fill densities
  and link IDs for seeded ingredients, and to offer matches for unknown ones.
  It never overwrites a curated value.
- **Auto-creation:** when ingestion meets an ingredient it cannot match, it
  proposes a new canonical ingredient on the review screen, which is saved with
  `reviewed = false`. The catalog page lists unreviewed ingredients for
  cleanup.
- **Merging:** two canonical ingredients can be merged; lines pointing at the
  removed one are re-pointed.

The ingredient name excludes qualifiers: "melted butter" is the ingredient
"butter" with the preparation qualifier "melted".

## Consequences

Matching quality depends on aliases, which grow as recipes are ingested. The
USDA data is large; the import is an offline command, not a start-up step.

**Obligations:**

1. The seed loads by migration and contains aisle and aliases for every entry.
2. The USDA import is idempotent and never overwrites a curated field.
3. Ingestion never silently creates an ingredient; it appears on the review
   screen first.
4. Merging two ingredients re-points every referencing line, in one
   transaction.
