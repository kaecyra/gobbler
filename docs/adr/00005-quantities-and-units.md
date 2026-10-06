# ADR-00005: Exact quantities, unit dimensions, density conversion, scaling rules

**Status:** Accepted

**Date:** 2026-10-06

## Context

Recipes write quantities as "1 1/2", "⅓", "2-3", "a pinch", "to taste", "1
(14 oz) can". Scaling must turn "1/3 cup" × 3 into "1 cup", not "0.9999 cup".
Shopping lists must add "2 tbsp butter" to "1/2 cup butter", and, where the
owner asks for metric or weight, turn "1 cup flour" into grams.

Options considered:

- **Floating-point quantities.** Accumulates error that shows up as "0.333
  cup". Rejected for storage and arithmetic; floats appear only at display.
- **Store quantities as text.** Cannot scale or aggregate. Rejected.
- **Exact rationals with explicit dimensions.** Chosen.

## Decision

### Quantity

A quantity is an exact rational (numerator/denominator) or a range of two
rationals. Unicode fractions, mixed numbers and decimals are all parsed into
rationals. A line may have no quantity ("salt to taste").

### Unit

Each unit has a canonical name, aliases ("T", "tbsp", "tablespoon"), a system
(US customary, metric, or none) and a **dimension**: `mass`, `volume`,
`count`, or `other` ("pinch", "clove", "can", "stick"). Within a dimension,
each unit has an exact factor to a base unit (gram, millilitre).

`other` units do not convert. A unit with a parenthetical size ("1 (14 oz)
can") stores the package size as a secondary quantity so the shopping list can
total it.

### Conversion

- Within a dimension: always possible.
- Volume to mass: only when the canonical ingredient has a density
  (ADR-00008). Never guessed.
- Display modes: **as written** (default), **metric** (volume → mL/L, mass →
  g/kg), and **weight** (volume → mass where a density exists, otherwise
  metric). The stored recipe never changes; conversion is a view.
- Displayed values are rounded to kitchen-sensible precision (nearest common
  fraction for US units, sensible grams for metric), with the exact value
  available.

### Scaling

A recipe is scaled by a rational multiplier, or by choosing a target serving
count. Each ingredient line carries a scaling rule:

- `linear` (default): quantity × multiplier.
- `fixed`: unchanged ("1 bay leaf", "pinch of salt").
- `to_taste`: no quantity, unchanged.

After scaling, the view picks a more readable unit in the same system where one
exists (48 tsp → 1 cup). Equipment and times are not scaled, but a scaled view
shows a hint on equipment with a size ("may need a larger pan") and on cook and
special times ("time may change").

## Consequences

Parsing has to produce rationals and units reliably, which ADR-00006 owns. The
unit table and its aliases are seed data and grow over time.

**Obligations:**

1. Quantity arithmetic and conversion are pure functions in one package with
   table-driven tests covering fractions, mixed numbers, ranges and unicode
   fractions.
2. Scaling "1/3 cup" × 3 yields exactly "1 cup".
3. A volume-to-mass conversion for an ingredient without a density leaves the
   value in volume.
4. `fixed` and `to_taste` lines are unchanged by any multiplier.
