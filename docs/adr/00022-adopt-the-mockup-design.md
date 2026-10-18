# ADR-00022: Adopt the reviewed mockup as the app's visual design, with the recipe fields it needs

**Status:** Amended by [ADR-00024](00024-equipment-chips-icons-and-catalog.md), [ADR-00025](00025-one-cuisine-per-recipe.md)

**Date:** 2026-10-17

## Context

Every page the app serves today is close to unstyled: one small shared
stylesheet and a few per-feature ones. The owner reviewed a UI mockup over
eight rounds (five screens: library with search, recipe, cook mode, shopping,
add and review) and settled its look, but no record adopted it, so no plan
node built it. Shown the running app, the owner asked where the design went.

The mockup draws on data the recipe model ([ADR-00004](00004-recipe-data-model.md))
does not hold: a meal type that groups the library, a spice level, a time
ribbon coloured by what each step is doing (rising, baking, simmering), step
titles, and pinned tips. ADR-00004 gives a step only text, a duration and an
active/passive flag.

The mockup also carries its art inline: six mascot drawings in a hidden SVG
sprite of about half a megabyte, repeated on every screen. It loads no fonts,
scripts or images from any other origin, so it sits inside the
no-third-party-assets boundary ([ADR-00002](00002-single-binary-go-stack.md))
as drawn.

Options considered:

- **Restyle only, no new fields.** The library cannot group by meal and the
  ribbon cannot colour its stages, which are the parts the owner valued most.
  Rejected.
- **Hold meal type and spice level as free-text tags.** Tags exist, but a
  grouping needs a closed set and a tag can be misspelt or missing. Rejected.
- **Replace the active/passive flag with a stage kind.** Cleaner in the end,
  but the flag drives "meanwhile" guidance, cook mode and the step-graph
  gap fill ([ADR-00020](00020-ingest-step-graph-gap-fill-review.md)); changing
  its meaning touches all of them for no gain in what the page shows.
  Rejected: the kind is added alongside it.
- **Mock up every remaining page before building any.** The editor, settings,
  household, sharing, catalog, pantry and operator pages have no design.
  Rejected by the owner: they take the same tokens and components, and are
  reviewed from screenshots in their pull requests.
- **Serve the mascots inline, as the mockup does.** Half a megabyte on every
  page, uncached. Rejected: each mascot is its own embedded SVG file.

## Decision

1. **The mockup is the visual specification.** Its source is kept in the
   repository under `docs/ui/`, beside a written specification
   (`docs/ui/design.md`) of its tokens, components and the owner's settled
   choices. Where the two disagree, the written specification wins.

   > Amended by [ADR-00024](00024-equipment-chips-icons-and-catalog.md):
   > equipment renders as chips with icons, not the mockup's rows.
2. **Light by default.** The theme never follows the operating system's
   colour scheme; dark is an explicit toggle remembered by the browser.
   Tablet on a kitchen counter is the first layout, then phone, then laptop.
3. **Corners are 4–6px.** The mockup's 12px card radius is not carried over.
4. **New recipe fields.** All are optional, part of the recipe revision, and
   shown and editable on the review screen and in the editor:
   - **Meal type**, one of `breakfast`, `main`, `side`, `dessert`, `snack`,
     `drink`. Ingest maps schema.org `recipeCategory` onto it when the mapping
     is unambiguous and leaves it empty otherwise.
   - **Spice level**, 0 to 3. Never set by ingest.
   - **Step stage kind**, from a closed set (`prep`, `rise`, `rest`, `steep`,
     `marinate`, `chill`, `bake`, `roast`, `simmer`, `braise`, `cook`), beside
     the existing active/passive flag, which keeps its meaning. Rule-based
     ingest proposes a kind from the step text; an unknown kind is empty, not
     an error ([ADR-00006](00006-rule-first-ingestion.md)).
   - **Step title**, short free text, taken from the source when it has one.

   > Amended by [ADR-00025](00025-one-cuisine-per-recipe.md): a recipe also
   > has an optional cuisine from a fixed list.
5. **A tip is a pinned note.** A note may be pinned as a tip and is then shown
   in the recipe view. Ingest never creates tips.
6. **The library's side panels are built:** a suggestion, recently made, and
   haven't made in a while, all read from the made-it log.
7. **The time ribbon's segments are computed on the server**; a small embedded
   script only fits them to the card width.
8. **Deferred:** "during step N" dependencies, a change summary on each
   revision, and named step timers. Each needs schema for a small gain.
9. **Pages without a mockup** take the same tokens and components and are
   reviewed from screenshots.

## Consequences

ADR-00004's recipe and step definitions gain fields; this amends its Recipe
and Steps sections. The shopping list, cook mode, history, journal and every
other page are restyled, which touches every web subpackage
([ADR-00012](00012-web-subpackage-per-feature.md)).

**Obligations:**

1. `docs/ui/mockup.html` holds the mockup source, and `docs/ui/design.md`
   holds the tokens, components and settled choices, with a row for the
   frontend in `AGENTS.md`'s on-demand table pointing at a new
   `.claude/rules/frontend.md`.
2. A schema migration adds meal type, spice level, step stage kind, step
   title and the pinned-note flag, each with a CHECK constraint on its closed
   set or range; a store test shows each survives an edit, a restore and a
   shared copy.
3. No stylesheet or template keys the theme off `prefers-color-scheme`; an
   end-to-end test shows a dark-scheme browser renders the light theme until
   the toggle is used.
4. Each mascot is served as an embedded SVG file; no page inlines a mascot
   drawing.
5. Ingest fixtures show `recipeCategory` mapped to a meal type in the
   unambiguous case and left empty otherwise, and stage kinds proposed from
   step text; spice level and tips stay empty after ingest.
6. ADR-00004's Recipe and Steps sections carry amendment notes pointing here,
   and its Status line and index row name ADR-00022.
