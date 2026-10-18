# ADR-00025: A recipe has at most one cuisine, from a fixed list

**Status:** Accepted

**Date:** 2026-10-18

## Context

The owner wants to record a recipe's cuisine (Japanese, Mexican, Italian and
so on). The recipe model ([ADR-00004](00004-recipe-data-model.md), amended by
[ADR-00022](00022-adopt-the-mockup-design.md)) has a meal type and spice level
but nothing for cuisine. Many sources state one: schema.org `recipeCuisine`,
such as "Japanese" on the owner's first test page.

Options considered:

- **Free text.** It records whatever the source or the cook types. It drifts
  into "Asian", "asian" and "Asian-inspired" as separate values, which defeats
  grouping by cuisine.
- **Several cuisines from a fixed list.** This handles fusion, but every
  display and grouping then has to place one recipe in several groups. The
  owner chose the simpler shape.
- **One cuisine from a fixed list, optional.** Chosen.

## Decision

1. A recipe revision carries an optional cuisine: one value from a fixed list
   defined in code, including "Other". The list starts with American,
   British, Caribbean, Chinese, French, German, Greek, Indian, Italian,
   Japanese, Korean, Mediterranean, Mexican, Middle Eastern, Spanish, Thai,
   Vietnamese and Other. Adding a cuisine is a code change.
2. The cuisine is part of the revision, like meal type, so history, restore
   and a shared copy carry it ([ADR-00018](00018-households-as-tenants.md)
   Sharing).
3. The editor sets and clears it. The recipe page and library cards show it.
4. Ingest maps schema.org `recipeCuisine` onto the list by rule: a
   case-insensitive name match, plus an explicit synonym table. A value that
   matches nothing leaves the cuisine empty rather than guessing. It is shown
   on the review screen ([ADR-00006](00006-rule-first-ingestion.md)).

## Consequences

This amends ADR-00004's revision contents and ADR-00022's recipe fields with
one optional field. It needs a schema migration, and ingest output changes, so
the parser version bumps.

**Obligations:**

1. Add a cuisine column to `docs/design.md`'s recipe-revision description.
2. Set the index rows for ADR-00004 and ADR-00022 in `docs/adr/README.md` to
   note "Amended by 00025".
