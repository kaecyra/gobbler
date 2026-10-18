# ADR-00024: Equipment shows as chips with icons, and the central catalog covers common kitchen equipment

**Status:** Accepted

**Date:** 2026-10-18

## Context

The recipe page and cook mode list equipment as plain text rows. The
reviewed mockup ([ADR-00022](00022-adopt-the-mockup-design.md)) draws an icon
beside each equipment item but keeps them as rows. The owner asked for
equipment to read as items: chips at least, with icons. The owner also asked
that the central catalog ([ADR-00018](00018-households-as-tenants.md) Catalog)
hold all the common cooking and baking equipment. Today it seeds a short list
that lacks everyday tools such as a sieve, thermometer, scale, tongs, pie dish
or bundt pan.

Options considered:

- **The mockup's rows with icons.** These are already reviewed, but the owner
  preferred chips.
- **Icons only, no catalog growth.** This leaves ingest and the editor
  matching against a catalog that misses common tools, so lines fall through
  to unreviewed household entries.
- **Chips with icons, and a larger catalog.** Chosen.

## Decision

1. Equipment on the recipe page and in cook mode renders as chips: an icon,
   the name, and the size hint where one applies
   ([ADR-00019](00019-sized-equipment-flag.md)).
2. Every central catalog equipment item has a line icon in the mockup's style
   (24px grid, `currentColor`). Anything without one, including a
   household-overlay item, shows a generic equipment icon. The icons live in
   `internal/web/layout` ([ADR-00012](00012-web-subpackage-per-feature.md)).
3. A new migration grows the central catalog to cover common cooking and
   baking equipment: pans and bakeware by type, knives, hand tools, measuring
   tools and small appliances. Names follow the existing seed. The `sized`
   flag is set where size matters.

## Consequences

This amends ADR-00022 for the equipment section only: chips replace the
mockup's rows. The catalog grows only through migrations, so an icon is added
alongside each new central item.

**Obligations:**

1. A test enumerates the central catalog's equipment and fails if any item
   has no icon.
2. `docs/ui/design.md` states that equipment renders as chips with icons.
   ADR-00022 carries an amendment note at decision 1 and names this record in
   its Status line, and its index row in `docs/adr/README.md` says "Amended by
   00024". All three land with this record.
