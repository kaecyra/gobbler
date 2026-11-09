# ADR-00051: A saved recipe's picture is chosen on its edit page, from a search of its source page or an upload

**Status:** Accepted

**Date:** 2026-11-09

## Context

ADR-00039 lets a recipe's picture be chosen at import review, and ADR-00042
adds the same choice to a re-import. Once a recipe is saved there is no other
way to change it. ADR-00039, "Changing a picture later", put the server
operation in place (upload, re-find against the saved `html` snapshot, remove)
and built no UI for it, so the picture service's re-find has no caller and the
recipe editor at `/recipes/{id}/edit` has no picture controls.

The owner hit this with three recent imports that got no picture: the page
offered nothing that passed the candidate filters at the time, and the only way
to give those recipes a picture was a full re-import, which also re-reads the
whole recipe.

The decisions below were made with the owner on 2026-11-09, from two mockups
of the edit page (kept with the pull request that builds this).

Options considered for the controls:

- **Action tiles in the picker row.** "Find on source page" and "Upload" drawn
  as tiles beside the choices, as the first mockup had them. The owner
  rejected this because a tile in that row reads as a choice, and these start
  an action.
- **Buttons.** Chosen by the owner. The tiles are only choices: the current
  picture, what was found or uploaded, and "No picture".

Options considered for where the buttons go:

- **Beside the "Picture" heading**, at the right of the row's label, where
  they stay put whatever the tiles show.
- **Quiet links under the tiles**, where the eye lands after scanning the row.
  Chosen by the owner.

Options considered for what a search reads:

- **The saved `html` snapshot only**, as ADR-00039's re-find does. It costs no
  fetch, but a recipe whose snapshot named no usable picture gets nothing new,
  which is the case of the three imports above.
- **The live page only.** It sees what the site offers today, but finds
  nothing when the site is down or has moved the recipe.
- **The live page, then the snapshot when the live fetch fails.** Chosen by the
  owner.

Options considered for agents over MCP:

- **The agent picks the picture through a review proposal.** ADR-00039 left
  this as "a later decision", because a proposal would have to carry media,
  and the person would review a picture the agent chose.
- **The agent finds pictures and the person picks.** Chosen by the owner. The
  agent's search fills the same offered set the edit page shows, and the choice
  stays with the person on that page.

Options considered for a recipe with no picture:

- **Search only when asked.** The person opens the edit page, sees "No
  picture" and has to know to press the search link.
- **Search on its own when the edit page opens** for a recipe with a source
  page, no picture and nothing already offered. Chosen by the owner. The page
  renders at once and the search's results take the place of two placeholder
  tiles when it answers.

Options considered for how the edit form names the offered picture it chose:

- **By position in the row**, as the review screen names a draft's
  candidates. Rejected on review of the first build: a second search, from
  another tab or an agent's `find_recipe_pictures`, refills the same positions
  in a new order, so saving a choice made on the page would apply whichever
  picture now holds that position. The owner: "pictures should have IDs".
- **By an ID of the picture's own.** Chosen. Each offered picture gets an
  opaque ID when it is found or uploaded, and no later search or upload reuses
  it.

## Decision

### The picker

- The edit page shows the review screen's picture picker above the form. Its
  tiles are only choices: "Current picture" (captioned "Left as it is"), chosen
  by default, when the recipe has one; each offered picture, captioned "Recipe
  image" or "Uploaded picture" with its size; and "No picture", captioned
  "Removes it" when the recipe has a picture and "Shows a coloured band" when
  it has none, and chosen by default then. With a picture the current one comes
  first and "No picture" last; without one "No picture" comes first.
- Under the tiles are two quiet links, "Search the source page again" and
  "Upload a picture". A recipe with no source URL gets no search link. While a
  search runs the link reads "Searching the source page" with a spinning icon,
  and two shimmering placeholder tiles stand where the results will go. A
  search that finds nothing shows "The source page had no usable pictures.",
  and one that fails shows "Could not search the source page." with the
  picker back at rest and the search link still there; neither offers a retry
  button. The picker adds no other text.
- A search, and a refused upload, keep the picker's current choice, unless a
  search replaced the found picture chosen, which falls back to the default. Without scripts, a search
  sends the browser back to the edit page with its outcome, and that page
  starts no search of its own.
- The choice is a field of the edit form. It is applied only when the form is
  saved, after the recipe itself is saved, and leaving without saving changes
  nothing. An offered picture is chosen by its ID.

### Searching

- A search fetches the recipe's source URL live through the guarded fetcher
  (ADR-00039, "Fetching"), with the same page fetch an import makes, and runs
  the import's extractor and candidate step on it. If the live fetch fails it
  reads the recipe's newest saved `html` source snapshot instead. It stores no
  new snapshot, and ADR-00004's snapshot rules are untouched.
- The images are downloaded fresh and filtered by the candidate rules of
  ADR-00039 as amended by ADR-00050, because the search is the picture
  service's own candidate step, generalised from ADR-00039's re-find rather
  than written a second time.
- Opening the edit page of a recipe with a source URL, no picture and nothing
  offered starts a search on its own, after the page has rendered.

### Offered pictures

- What a search finds, and a picture uploaded on the edit page, are kept per
  recipe as offered pictures: media objects under
  `recipes/<recipe_id>/found/<id>.webp`, plus rows in a table of the picture
  package's own, `recipe_found_pictures`. Each has an opaque random ID, never
  reused, that the picker's radio, the picture's address and the save use. A
  new search replaces what the last search found; a new upload replaces the
  last upload. The new pictures are stored before the old ones are removed, so
  a search or upload that fails leaves the previous pictures offered.
- Save checks the chosen ID and applies it in one operation, holding the
  recipe's lock across the recipe's write; replacing, sweeping and deleting
  offered pictures take the same lock. A choice whose ID is no longer offered,
  because a later search or upload replaced it or it expired, is refused with a
  message before the recipe is written, and is never resolved to another
  picture. A search's fetch and downloads run before the lock is taken.
- They last until the edit form is saved or 14 days pass, whichever comes
  first. On save the chosen one becomes the recipe's picture under
  `recipes/<recipe_id>/<version>.webp` (ADR-00039, "Storing") and every
  offered picture is deleted. Expired ones are swept with the proposals
  (ADR-00035). Deleting the recipe or its household deletes them, rows and
  objects, the way ADR-00039 deletes pictures.
- An upload has the limits and refusals of an avatar (ADR-00033). A refused
  file is said in the picker; an accepted one becomes a tile, chosen.

### MCP

- An MCP tool, `find_recipe_pictures`, takes a `recipe_id`, runs the same
  search, keeps what it finds as the recipe's offered pictures, and returns how
  many it found and the recipe's edit path. It never sets or removes a picture,
  so it is not a recipe write and needs no review (ADR-00034). It is recorded
  as a credential write like the other tools that change stored data.

## Consequences

This amends ADR-00039, "Changing a picture later": the operation now has its
UI, and re-find reads the live page before the snapshot. ADR-00039's rule that
no MCP tool sets, changes or removes a picture stands. The recipe service
(ADR-00043) gains the search, upload, list and apply operations, and MCP reaches
the picture service only through it.

A search reaches the source site each time it runs, including the automatic
one on an edit page whose last search found nothing.

**Obligations:**

1. ADR-00039's "Changing a picture later" carries an "Amended by ADR-00051"
   note, and its README index row names ADR-00051.
2. A migration adds `recipe_found_pictures`, cascading from its recipe and
   household.
3. Tests in `internal/picture` show a search reading the live page; a search
   falling back to the saved snapshot when the live fetch fails; a search that
   finds nothing returning no error; a new search replacing the found set and
   keeping the upload; an upload refused for a bad file and kept for a good
   one, with the recipe's picture unchanged; a failed store keeping the
   previous pictures; save applying a chosen offer, "No picture" and "Current
   picture", each deleting every offered picture; a choice a second search
   replaced refused before the save runs, with no picture applied;
   an offer older than 14 days no longer offered and removed with its object
   by the sweep while a younger one stays; a recipe whose sweep fails not
   stopping the sweep of the others; recipe and household deletion
   removing offered objects; and another household's recipe being not found.
4. Tests in `cmd/gobbler` show the start-up sweep deleting an expired offered
   picture's row and object, and the proposal sweep and the offered-picture
   sweep each running when the other fails, with both errors returned.
5. Tests in `internal/web/editor` show the automatic search on load, the
   picker of a recipe with a picture, no search link without a source URL, a
   search answering with its finds or the nothing-found note, an upload
   chosen and a refused one, save applying each choice, a save naming an offer
   that is gone refused with nothing written, and a picker rendered, searched
   again and saved with its old choice refused without applying the new set's
   picture in its place; a search keeping the choice, or falling back when it
   replaced the chosen picture; a failed search saying so and leaving the
   picker at rest; and a search without scripts carrying its outcome to a page
   that does not search again.
6. A test in `internal/web/picture` shows an offered picture served to its
   household only, by its ID.
7. An MCP test shows `find_recipe_pictures` keeping what it found, returning
   the count and edit path, and leaving the recipe's version and picture
   unchanged with no proposal made; and another household's recipe refused.
8. e2e covers `POST /recipes/{id}/edit/picture/search`,
   `POST /recipes/{id}/edit/picture/upload` and
   `GET /recipes/{id}/picture/offered/{offer}`, in a spec of the edit page and
   in the tenancy probes, and the spec refuses a choice a later search
   replaced.
9. `docs/operations.md` lists `find_recipe_pictures` in the MCP section, and
   the MCP server's instructions mention it.
10. `docs/ui/design.md` describes the edit page's picker.
