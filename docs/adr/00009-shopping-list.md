# ADR-00009: Shopping list aggregation, pantry, live view, Todoist push

**Status:** Accepted

**Date:** 2026-10-06

## Context

Making shopping lists from recipes is one of the main reasons gobbler exists.
The list is built at a desk and used on a phone in a store, sometimes by more
than one person at once.

Options considered for getting the list onto a phone:

- **Text export only.** Works anywhere, but checking items off is lost.
- **A live list page in gobbler.** Works through the same Access protection,
  with checks shared across devices.
- **Push to an external list app.** The owner wants Todoist. Apple Reminders
  was considered and dropped: it has no public web API, and an iOS Shortcut
  bridge was declined.

All three except Reminders are chosen.

## Decision

### Building a list

A shopping list holds **recipe entries** (recipe + multiplier) and **manual
items** (free text, or a canonical ingredient with an optional quantity).
Generating the list:

1. Scales each recipe's lines by its multiplier (ADR-00005).
2. Drops optional lines unless included, and lines whose ingredient is in the
   **pantry** (ingredients always on hand, such as salt and oil). Dropped
   pantry items are listed separately so they can be added back.
3. Merges lines by canonical ingredient. Quantities in the same dimension are
   summed into one; quantities in different dimensions that cannot be
   converted stay as separate amounts on one item ("2 cups + 3 cloves").
4. Groups items by aisle.

Each item shows which recipes it came from. Regenerating after a recipe change
keeps check state for items that still exist.

### Using a list

- **Live view:** a phone-friendly page; checking an item updates every open
  view through server-sent events.
- **Text export:** plain text grouped by aisle, for copying anywhere.
- **Todoist push:** creates or updates a Todoist project or section with one
  task per item, using a Todoist API token from the environment. Pushing is
  one-way; checks made in Todoist are not read back.

## Consequences

The pantry is a simple "always have it" set, not inventory with quantities.
Todoist sync is one-way to keep it simple.

**Obligations:**

1. Merging "2 tbsp butter" and "1/2 cup butter" yields one item with an exact
   sum.
2. Pantry ingredients are excluded from generated items and listed separately.
3. Checking an item in one browser updates another open view without reload.
4. Pushing the same list to Todoist twice does not duplicate tasks.
