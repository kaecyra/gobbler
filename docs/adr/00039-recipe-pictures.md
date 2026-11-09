# ADR-00039: A recipe's picture is chosen from its source page, kept as media, and shown on cards and the recipe header

**Status:** Amended by [ADR-00040](00040-loopback-fetches-under-the-dev-bypass.md), [ADR-00050](00050-picture-candidates-fall-back-to-1x.md)

**Date:** 2026-11-05

## Context

The owner wants a recipe's main image from its source page shown at the top of
the library card, full-bleed, fading out where the card's text starts.
ADR-00011 kept recipe photos out, and ADR-00021 allowed video thumbnails
without making them precedent for photos. ADR-00033 settled where uploaded
media lives (an S3-compatible bucket with a local-directory fallback, served by
the app after admission) and said it covered what people upload, not what the
app fetches. The decisions below were made one at a time with the owner; the
working record, with the evidence checked against the code, is
`docs/proposals/recipe-images.md`.

Pages offer two kinds of candidate. The recipe's structured data has an `image`
field, often a list, and the page has an `og:image` chosen for link previews.
Recipe-card plugins list small crops first and often append the original last,
and some sites list only crops and keep the full image in the page's primary
`ImageObject` and `og:image`. The extractor already reads the first structured
entry, which on the sites in the test fixtures is a crop between 225 and 500
pixels on a side. Author headshots and site logos also appear as `image` in
the same structured-data graph.

Options considered for storage:

- **SQLite blobs, like video thumbnails.** A fetched image cannot be recreated
  once the source site changes, which argues for keeping it in the nightly
  snapshot. Rejected because each picture is roughly 80 to 200 KB, so a few
  hundred recipes add tens of megabytes to the database and every snapshot, and
  because it would be a second path for media beside `internal/media`.
- **Hotlinking the source URL.** Rejected: it breaks the no-remote-assets
  boundary (AGENTS.md), reports every card view to the source site, and fails
  on hotlink protection and link rot.
- **The media store of ADR-00033.** Chosen.

Options considered for object keys:

- **Household first** (`households/<id>/recipes/<id>/…`), so deleting a
  household is one prefix delete. Rejected because it records ownership a
  second time; the database already decides tenancy and admission checks it on
  every request.
- **Kind first** (`recipe-images/…`, with avatars left at `avatars/…`).
  Rejected because each new kind of media adds a top-level name and the bucket
  stops reading like the data model.
- **Keys named for the owning recipe or person.** Chosen. Recipe ids are
  globally unique, so the household adds nothing to the key.
- **Recipes as free-standing objects associated with households.** The owner
  raised this and set it aside. It would reverse ADR-00018's ownership and
  copy-on-share model, and this record changes key names only.

Options considered for choosing the picture:

- **`og:image` first, falling back to the last structured entry.** Right on
  every fixture that has both, but wrong whenever `og:image` is a banner or a
  collage, and nobody is asked.
- **Structured data first.** Picks a crop on sites that list only crops.
- **`og:image` only.** No picture for a page that names one only in its recipe
  data.
- **Fetch every candidate and keep the largest automatically.** The result
  depends on what the network returned, and the largest image is not always
  the right one.
- **The person chooses from every appropriate candidate.** Chosen by the owner.

Options considered for the revision history:

- **The picture key inside the recipe document, like the video ID.** Rejected:
  restoring an older revision would point at an object deleted when the picture
  was replaced, unless no picture were ever deleted.
- **The picture key outside the document.** Chosen.

Options considered for fetching URLs a page names:

- **Guard the image fetch only.** Rejected: the page fetch follows redirects,
  so a URL a member typed can already redirect to a private address, and this
  would leave two fetch policies.
- **No guard, relying on candidates having to decode as images.** Rejected: the
  request still reaches devices on the network the server runs in.
- **Refuse all redirects, as the OAuth metadata fetch does.** Rejected for
  recipe pages, which commonly redirect once from `http` to `https` or to a
  `www` host.
- **One guard for every fetch made on someone's behalf.** Chosen.

Options considered for where the picture shows:

- **Library cards only.** Rejected: the card and the page it opens stop
  looking like the same recipe.
- **Everywhere a recipe appears.** Rejected: it crowds the home side panels,
  whose suggestion card already holds a mascot head, and spends cook mode's
  space.
- **Library cards and a short recipe page header.** Chosen.

Options considered for a recipe with no picture:

- **No band, so the card starts with its text.** Rejected: a library mixing
  cards with and without pictures becomes a ragged grid.
- **The mascot.** Rejected: repetitive across a grid, and the mascot is never
  clipped (`docs/ui/design.md`), which a band cropped to a fixed height would
  do.
- **A placeholder variant picked at random on each page load.** Rejected: a
  card would change on every visit, and screenshots could not be repeated.
- **A placeholder band, its variant chosen from the recipe id.** Chosen.

## Decision

### Choosing

- A URL import collects candidates in this order: the page's `og:image`; every
  entry of the recipe's own structured-data `image` (JSON-LD or microdata); and
  the page's primary `ImageObject` where the structured data names one. Other
  images on the page or in its structured-data graph are not candidates.
- Duplicate URLs are dropped, keeping the first. A candidate that fails to
  download or decode is logged and dropped; it never fails the ingest
  (error-handling rule: low confidence is data). A candidate too small to fill
  the card is dropped, with the minimum taken from the approved mockup.
  Candidates whose decoded pixels are identical collapse to one. At most 8
  remain. Size variants that survive are kept as separate options, because
  they are different crops.

  > Amended by [ADR-00050](00050-picture-candidates-fall-back-to-1x.md): the
  > minimum is the widest card at 2x when any candidate meets it, and the card
  > at 1x otherwise. Deduplication and the cap of 8 apply after the minimum.
- The review screen shows the candidates as small options at the top of the
  draft. The person must choose one of them or "No picture" before confirming,
  and "No picture" is always offered. With no candidates, which is always the
  case for pasted text and photo imports, it is the only option.
- An agent's draft over MCP (ADR-00034) gets the same candidates and waits for
  the same choice. No MCP tool sets, changes or removes a picture; one that
  proposes a picture for review needs proposals that carry media, which is a
  later decision.

### Storing

- Pictures are processed by `internal/media`: decoded, scaled to fit within
  1024 by 1024 pixels with nothing cropped, and re-encoded as WebP with no
  metadata (ADR-00033). They are kept through the media store's interface in
  the S3 media bucket, or the local directory without S3, and served by the
  app after admission. No page points at the source site's image.
- Object keys are named for what owns them:

  ```
  recipes/<recipe_id>/<version>.webp
  drafts/<proposal_id>/<n>.webp
  people/<person_id>/avatar/<version>.webp
  people/<person_id>/uploads/<token>.orig        (and .preview.jpg)
  ```

  `<version>` is a new random name each time a picture changes, so a served
  picture can be cached as immutable. Avatars move to the `people/` keys.
- While a draft waits, its candidates live under `drafts/<proposal_id>/`.
  Confirming copies the chosen one to `recipes/<recipe_id>/` and deletes the
  draft's prefix. A discarded, superseded or expired draft's candidates are
  deleted no later than the proposal is pruned (ADR-00035).
- The database records each picture's key outside the recipe document, written
  by statements of its own, like the optional fields of ADR-00022. A picture
  change is not a revision, and restoring a revision leaves the picture as it
  is. Replacing a picture deletes the previous object.
- Deleting a recipe deletes its picture. Deleting a household collects its
  recipes' picture keys before the database cascade and deletes the objects
  after the commit. A failed object delete is logged and does not fail the
  deletion, since what remains is unreachable bytes.

### Changing a picture later

The server offers one operation on any recipe, with no UI built for it yet. It
can upload a file as the picture, with the same limits and refusals as an
avatar (ADR-00033); re-find candidates by running the candidate step above
against the recipe's saved `html` source snapshot and returning them for a
choice; or remove the picture. Setting a picture from a typed URL is not
offered. Recipes imported before this record are not backfilled.

The candidate, fetch, processing and storage logic lives in domain packages,
and handlers only parse the request, call it and render. The owner has asked
for the work behind the handlers to become a discrete API, which is a separate
decision (`docs/proposals/discrete-api.md`); keeping this logic out of handlers
lets it move behind that boundary unchanged.

### Fetching

- The address guard of the OAuth metadata fetch (ADR-00034) moves to a package
  of its own and serves three fetches: the OAuth metadata document, the recipe
  page, and the image candidates.
- It refuses to connect to any address that is not public unicast, checked
  when the connection is dialled, after DNS resolution. It ignores proxy
  environment variables so that it sees the address actually dialled.

  > Amended by [ADR-00040](00040-loopback-fetches-under-the-dev-bypass.md):
  > loopback is admitted for the page and image fetches while the dev auth
  > bypass is on.

- Recipe page and image fetches follow at most 5 redirects; each hop must be
  `http` or `https`, and each hop's connection passes the same guard. The
  OAuth metadata fetch still refuses every redirect.
- A refused page fetch is a fetch error with a message the importer can read.
  A refused image candidate is logged and dropped.

### Showing

- **Library and search cards** show the picture full-bleed at the top, fading
  into the card where the text starts.
- **The recipe page header** shows it short, with the title over the fade, so
  the ingredients stay high on a tablet.
- Cook mode, the home side panels, shopping lists, and the journal and history
  pages show no picture.
- A recipe with no picture shows a placeholder band at the same height, drawn
  from the design tokens and carrying the recipe's meal-type picture. The band
  comes in a set of variants, and a recipe's variant is its id modulo the size
  of the set.
- A recipe whose picture object is missing from the store shows its
  placeholder band, and the missing object is logged.
- The faded card, the header, the review screen's picker and the placeholder
  variants are drawn in `docs/ui/mockup.html` and approved by the owner before
  they are built (lessons.md, "An approved mockup is the visual target").

## Consequences

This amends ADR-00011, whose "Images" deferral now admits recipe pictures
chosen from the source page or uploaded, and ADR-00033, whose storage now
covers images the app fetches as well as uploads. ADR-00021's video thumbnails
stay in SQLite. Pictures are not in the nightly database snapshot, and losing
the storage server loses them; for decoration that can be fetched or uploaded
again, that is accepted.

A recipe cannot be imported by URL from a host on the local network once the
guard covers the page fetch.

**Obligations:**

1. ADR-00011's "Images" bullet and ADR-00033's Consequences each carry an
   "Amended by ADR-00039" note at the affected passage.
2. One package holds the address guard. The OAuth metadata fetch, the ingest
   page fetch and the image candidate fetch use it, and no copy remains in
   `internal/oauth`. Tests show that a name resolving to a private address is
   refused for the page and image fetches, that a redirect from a public to a
   private address is refused, that a sixth redirect is refused, that a
   redirect to a scheme other than `http(s)` is refused, and that the OAuth
   fetch still refuses any redirect.
3. `docs/operations.md` says that gobbler's outbound fetches ignore
   `HTTP_PROXY` and `HTTPS_PROXY` and never reach private addresses, so recipes
   cannot be imported from hosts on the local network.
4. `internal/media` gains a scale-to-fit function that crops nothing, with the
   same no-metadata test as ADR-00033 obligation 3.
5. Avatar keys move to `people/<person_id>/…`; existing objects are not
   migrated.
6. The extractor parses `og:image` and the primary `ImageObject` and returns
   the ordered candidate list in place of a single image URL. The golden files
   are updated, and at least one fixture keeps its `og:image`.
7. Tests show that a failed candidate is dropped without failing the ingest,
   that confirming leaves only the chosen object, under `recipes/`, and that
   discarding a draft leaves no `drafts/<proposal_id>/` objects.
8. The picture key lives outside the recipe document. Tests show a revision
   restore leaves the picture unchanged; upload, including a refused file;
   re-find against a saved snapshot, and re-find on a recipe with no `html`
   snapshot returning no candidates; remove; replacing a picture deleting the
   previous object; and recipe and household deletes removing pictures.
9. A test shows a member of another household cannot fetch a recipe's picture
   or a draft's candidate by its address (ADR-00033 obligation 4, extended).
10. No MCP tool sets, changes or removes a recipe's picture.
11. The mockup in `docs/ui/mockup.html` adds the faded library card, the recipe
    page header, the review screen's picker and the placeholder variants, and
    the owner approves it before any node building that UI is dispatched.
12. `docs/ui/design.md` states where the picture shows, the fade, and the
    placeholder rules, matching the approved mockup.
13. e2e tests show a draft confirmed with a picture and with "No picture"; the
    picture on a library card and the recipe page; the placeholder for a recipe
    without one; no picture in cook mode; and the placeholder for a recipe whose
    picture object is missing. A unit test shows the placeholder variant is a
    pure function of the recipe id.
14. `docs/operations.md`'s statement that a lost media object shows a broken
    image is corrected for recipe pictures.
15. After the feature ships, the demo is reseeded through `seed.py` and the
    import path, so its recipes go through the picture choice.
