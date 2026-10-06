# ADR-00004: Recipe data model: components, ingredient lines, step graph, revisions

**Status:** Accepted

**Date:** 2026-10-06

## Context

The point of gobbler is recipes that are easy to find, easy to follow, and easy
to turn into a shopping list. All three depend on the recipe being structured
rather than stored as text: a shopping list needs to know that "2 tbsp butter,
melted" and "1/2 cup unsalted butter, at room temperature" are the same thing
to buy; scaling needs to know which numbers are quantities; following a recipe
with a sauce made while the dough rises needs to know which steps can overlap.

Options considered:

- **Store recipes as text or Markdown, parse on read.** Every feature re-parses,
  and edits cannot preserve meaning. Rejected.
- **A flat schema.org-shaped `Recipe` (one ingredient list, one step list).**
  Matches what sites publish but cannot express sub-recipes (a sauce, a
  topping) or step dependencies. Rejected as the stored model; it remains an
  ingestion source (ADR-00006).
- **A structured model with components and a step dependency graph.** Chosen.

## Decision

### Recipe

Name, description, source URL, author, yield (quantity + free-text unit, e.g.
"1 loaf"), servings, calories per serving (entered or scraped, not computed —
ADR-00011), tags, rating, and times.

**Times:** `prep`, `cook`, and zero or more **special** times, each with a
label (`rise`, `marinate`, `chill`, `rest`, or free text) and a duration.
`total` is stored when the source states it; otherwise it is computed from the
step graph's critical path when step durations are known, else from the sum of
prep, cook and special times. The UI shows which.

### Component

A recipe has one or more ordered components ("Dough", "Filling", "Sauce"). A
recipe with no natural parts has a single unnamed component. Each component owns
its ingredient lines and steps.

### Ingredient line

- **Quantity:** an exact value or a min–max range, or none (ADR-00005).
- **Unit:** a reference to a known unit, or none (count-only items: "2 eggs").
- **Ingredient:** a reference to a canonical ingredient (ADR-00008).
- **Qualifiers:** an ordered list, each typed as `preparation` ("melted",
  "finely chopped"), `state` ("at room temperature", "cold"), `form`
  ("powdered", "ground"), or `note` (anything else, e.g. "preferably
  homemade").
- **Optional** flag, and a **scaling rule** (ADR-00005).
- **Raw text:** the original line, retained for display and re-parsing.

### Equipment

A catalog of equipment ("stand mixer", "9x13 pan", "Dutch oven") linked to
recipes, optionally with a quantity, and optionally to the steps that use it.

### Steps and sequencing

Each step belongs to a component and has text, an optional duration, and an
`active` / `passive` flag (passive: rising, baking, marinating). Steps may
declare **dependencies on other steps in any component of the same recipe**.
The dependencies form a directed acyclic graph; a cycle is rejected on save.

The default linear order is component order, then step order. The view derives
"meanwhile" guidance from the graph: when a passive step starts, steps whose
dependencies are satisfied and which feed a later step are offered as work to do
during it ("while the dough rises: make the sauce").

### Notes, history and provenance

- **Notes:** free text, timestamped, many per recipe, authored by the user.
- **Made-it log:** date, optional rating and comment.
- **Revisions:** every save of an edit writes a revision holding the full
  recipe document as it was before the edit, with timestamp and author. Any
  revision can be viewed and restored (a restore is itself a new revision).
- **Clone:** copies a recipe into a new, independent recipe that records which
  recipe and revision it came from.
- **Source snapshot:** the ingested input (HTML, pasted text, OCR text, LLM
  response) is stored once, unaltered, linked to the recipe.

## Consequences

Ingestion has to produce this structure, which is the hard part of the project
and the subject of ADR-00006. Editing UI must handle components, qualifiers and
step dependencies, which is more work than a text area.

**Obligations:**

1. The schema has tables for recipes, components, ingredient lines, qualifiers,
   equipment, step dependencies, notes, made-it entries, revisions and source
   snapshots.
2. Saving a step graph with a cycle fails with a message naming the steps
   involved.
3. Every edit produces a revision; restoring a revision is tested to round-trip
   the full recipe.
4. Source snapshots have no update path in the store.
5. A clone records its origin recipe and revision.
