# Open decisions: recipe images from the source page

Owner request, 2026-11-05: import a recipe's main image (OpenGraph or structured data) and show it
full-bleed at the top of the recipe card, fading out where the text starts. This reverses ADR-00011's
"recipe photos stay out" and needs an ADR. Worked through with `/overseer:decide`; all decisions
are below. Ruled in [ADR-00039](../adr/00039-recipe-pictures.md), which carries the obligations;
the work is plan nodes #421–#430.

## Verified facts

- ADR-00011 keeps recipe photos out (`docs/adr/00011-v1-scope-and-deferrals.md:43-46`); ADR-00021
  says a thumbnail "is not precedent for photos" (`docs/adr/00021-youtube-video-on-a-recipe.md:87`).
- The structured-data image is already parsed into `extract.Raw.ImageURL`
  (`internal/ingest/extract/extract.go:97`, `jsonld.go:199`, `microdata.go:99`); nothing outside
  `extract` uses it. `og:image` is not parsed anywhere.
- `internal/media` decodes JPEG, PNG, GIF, WebP, HEIC and AVIF and encodes WebP in pure Go
  (`image.go:16`, `:55`), at most `MaxSide = 1024` (`:27`). `Avatar()` crops square (`:150`); a
  fetched image would need a fit-without-crop step.
- The structured-data `image` is read as the first entry of the list (`jsonld.go:199`), and on real
  pages that entry is usually a small square crop: recipetineats `_sq.jpg`, loveandlemons `500x500`,
  pinchofyum `fit=225,225`, bbcgoodfood `440,400` (the `real_*`/`plugin_*` golden files under
  `internal/ingest/extract/testdata/`). The recipetineats page's `og:image` is 680×952
  (`real_recipetineats_graph.html:40-42`).
- Ingest's page fetch has no private-address guard (`internal/ingest/fetch.go:30`). The OAuth
  metadata fetcher has one, as a dial `control` hook (`internal/oauth/metadata.go:133`).
- Avatars are stored under flat keys: `avatars/<id>.webp` and `avatars/uploads/<id>.orig|.preview.jpg`
  (`internal/profile/avatar.go:72`, `:337`).
- `recipes.id` is a global `INTEGER PRIMARY KEY`, with `UNIQUE (household_id, id)` for child
  foreign keys (`internal/db/migrations/00008_household_recipes.sql:46-68`).
- In the WPRM and Yoast fixtures the recipe's `image` list runs small crops first and ends with the
  unsized original (`real_wprm_graph.html`, `real_yoast_graph.html`). The recipetineats list is all
  `_sq` crops; its full image appears only as the page's `#primaryimage` and `og:image`. The other
  fixtures were trimmed of `og:image`, so they cannot show how common it is.
- Drafts are rows in `proposals` (integer `id`, `state` pending/accepted/rejected/superseded/expired;
  `internal/db/migrations/00027_recipe_proposals.sql:96-120`). A household holds at most
  `MaxPendingDrafts = 20`; pending lasts `PendingLifetime` (a year) and finished proposals are pruned
  `FinishedLifetime` (60 days) later (`internal/store/proposal.go:55-60`, prune at `:601`).
- A recipe's video ID is part of the recipe document (`internal/recipe/model.go:146`) but is written
  by statements of its own (`internal/store/queries/video.sql:1-7`); `recipe_fields.sql` follows the
  same pattern.
- No general HTTP API exists: the web handlers (HTMX, ADR-00012) and the MCP tools
  (`internal/mcp/`, ADR-00034) each call the domain packages (`store`, `ingest`, `recipe`, …)
  directly.
- The OAuth metadata fetcher guards its dial: `blockedAddr` refuses anything not public unicast,
  plus a list of reserved ranges (`internal/oauth/metadata.go:68-100`), checked in a `net.Dialer`
  `Control` hook after DNS resolution (`guardDial`, `:102`). It disables proxies so the guard sees
  the real address, and refuses all redirects (`:133-155`). The page fetch uses the default client
  with neither guard (`internal/ingest/fetch.go:44-49`).
- Households are joined only by invitation (ADR-00018, Invitations), so the person who types an
  import URL is a member.
- "Recipe card" is the library card on `/` and `/recipes`, with the time ribbon pinned to its bottom
  (`docs/ui/design.md:89`); the screens table is at `docs/ui/design.md:121-128`.
- URL imports keep the page HTML in `source_snapshots` (kind `html`, `content BLOB`;
  `internal/db/migrations/00027_recipe_proposals.sql:60-70`), so a backfill could read image URLs
  from saved pages.

## Open

(none: closed 2026-11-05. The discrete API moved to `docs/proposals/discrete-api.md`.)

## Decided

## D1 — A recipe's image is fetched once and kept in the media object store

**Decided.** The app downloads a recipe's image at import, processes it through `internal/media`
(decode, scale to fit, re-encode as stripped WebP) and stores it through `media.Store`: the S3
media bucket, which in the deployment is Garage, with the local-directory fallback for development
and e2e. It is served by the app after admission, like avatars (ADR-00033). No page points at the
source site's image.

**Why.** One media path: the store, the fallback and admission-gated serving exist for avatars, and
a second storage route for images would be two helpers doing one job. The image is decoration, not
personal data (AGENTS.md); losing it with the storage server costs a reimport, nothing that cannot
be recovered.

Rejected:
- **SQLite blob, like `video_thumbnails`.** Its real case: a fetched image cannot be recreated once
  the source site changes, so it arguably belongs in the nightly snapshot that ADR-00033 reserves
  for what cannot be recreated. Killed by size (roughly 80–200 KB each, so 50–100 MB per 500
  recipes in the database and every snapshot) and by being a second media path.
- **Hotlinking the source URL.** Breaks the no-remotely-loaded-assets boundary (AGENTS.md), reports
  every card view to the source site, and fails on hotlink protection and link rot.

**Obligations:**

1. The new ADR states that ADR-00033's storage covers images the app fetches as well as uploads,
   and amends ADR-00033's Consequences line "This record covers what people upload, not what the
   app fetches" to point at it.
2. `internal/media` gains a scale-to-fit function that keeps the whole picture (no crop), beside
   `Avatar()`, with the same no-metadata test as ADR-00033 obligation 3.
3. A test shows a member of another household cannot fetch a recipe image by its address
   (ADR-00033 obligation 4, extended).

## D2 — Media keys are named for the recipe or person that owns them, not the household

**Decided.** Object keys in the media store:

```
recipes/<recipe_id>/<version>.webp
people/<person_id>/avatar/<version>.webp
people/<person_id>/uploads/<token>.orig        (transient; likewise .preview.jpg)
```

`<version>` is a new random name each time the image changes, so a served image can be cached as
immutable and a replacement never shows stale. Avatars move off today's flat `avatars/<id>.webp` and
`avatars/uploads/` keys (`internal/profile/avatar.go:72`, `:337`). The data model is unchanged: a
recipe still belongs to exactly one household and sharing still copies (ADR-00018).

**Why.** Tenancy lives in one place, the database; admission checks it on every serve (ADR-00033).
`recipes.id` is globally unique, so the household adds nothing to the key except a second copy of
ownership that could drift. Each object sits under the thing it belongs to.

Rejected:
- **Household-first (`households/<h>/recipes/<r>/…`).** Its case was one prefix delete per household.
  Not worth recording tenancy twice.
- **Kind-first (`recipe-images/…`, `avatars/…` unchanged).** No avatar change, but every new kind of
  media adds a top-level name and the bucket does not read like the data model.
- **Recipes as free-standing objects merely associated with households.** Raised by the owner and set
  aside: that reverses ADR-00018's ownership and copy-on-share model, which rejected a live share.
  Paths only.

**Obligations:**

1. The database records each image's key. Deleting a recipe deletes its object; deleting a household
   collects its recipes' keys before the cascade and deletes the objects after commit. A failed object
   delete is logged and does not fail the deletion (the leftover is unreachable bytes, not exposed
   data). A test covers both deletes.
2. Avatar keys move to `people/<person_id>/…`. No migration of existing objects (no data anyone keeps).
3. The new ADR records this layout as the media key contract.

## D3 — The person chooses the recipe's picture from fetched candidates on the review screen

**Decided.** A URL import fetches every appropriate image candidate the page names, and the review
screen shows them as small options at the top of the draft. The person must choose one of them or
"No picture" before confirming. Confirming promotes the chosen candidate to the recipe and discards
the rest. With no candidates (pasted text, photo imports, or a page whose candidates all fail or are
filtered out) the screen offers only "No picture" and the recipe shows the placeholder.

Candidates, in this order:

1. The page's `og:image`.
2. The recipe's own structured-data `image` list (JSON-LD or microdata), every entry.
3. The page's primary image, where the structured data names one (a `#primaryimage` `ImageObject`
   in a Yoast-style `@graph`).

Not other images on the page or in the graph: author headshots and site logos also appear as
`image` (`Headshot-Main-Bio-Circle.jpg` in `real_yoast_graph.html`, `jeanine720x720.jpg` in
`real_wprm_graph.html`). Then, so only appropriate options are shown:

- duplicate URLs are dropped, keeping the first;
- a candidate that fails to download or decode is logged and dropped. That is low-confidence data,
  not a pipeline error, and never fails the ingest (error-handling rule);
- a candidate whose decoded image is too small to fill the card is dropped. The minimum is set
  from the approved mockup's card size (open item 5), not guessed;
- candidates whose decoded pixels are identical are collapsed to one;
- at most 8 remain.

Size variants that pass these filters stay separate options, because they are different crops.

**Where candidates live.** Processed like any fetched image (D1), under
`drafts/<proposal_id>/<n>.webp` in the media store; this adds a prefix to D2's layout. Confirming
copies the chosen one to `recipes/<recipe_id>/<version>.webp` and deletes the `drafts/` prefix. A
discarded, superseded or expired draft's candidates are deleted no later than the proposal's prune.
`MaxPendingDrafts` bounds the total at 160 candidate objects per household.

**Why.** No rule picks the right picture reliably: the first structured entry is a small crop on
every fixture, "last is largest" is a plugin habit, and `og:image` is occasionally a banner. The
person can tell at a glance. Choosing on the review screen also keeps ingestion deterministic: the
candidate list comes from the page, and nothing is ranked by what the network returned. "No
picture" is a required choice rather than an escape, so a draft whose candidates are all wrong can
still be confirmed.

Rejected (all automatic picks):
- **`og:image` first, structured last-entry fallback.** The dispatcher's lean. Right on every
  fixture that shows both, but wrong whenever `og:image` is a banner or collage, with no one asked.
- **Structured list first.** Picks a crop on recipetineats-style pages.
- **`og:image` only.** No picture for a page that has one in its recipe data.
- **Fetch every candidate and keep the largest automatically.** Same fetches as the chosen design,
  but the result depends on the network, and "largest" is not "right".

**Obligations:**

1. `extract` parses `og:image` and the primary `ImageObject`, and returns the ordered candidate list
   in place of the single `ImageURL` (`internal/ingest/extract/extract.go:97`). Golden files are
   updated, and at least one fixture keeps its `og:image`.
2. The review screen requires a picture choice, with "No picture" always offered; an e2e test
   confirms a draft each way.
3. Tests show: a failed candidate is dropped without failing the ingest; confirm leaves only the
   chosen object, under `recipes/`; discarding a draft leaves no `drafts/<proposal_id>/` objects.
4. An agent's draft over MCP (ADR-00034) gets the same candidates and waits for the same choice;
   nothing picks for it.

## D4 — No backfill; a picture can be set on any recipe through a server operation

**Decided.** Recipes imported before this change are not backfilled. The demo gets pictures by being
reseeded through the new import path. Separately, the server gains an operation that sets or changes
the picture of any recipe, existing ones included, so editing can use it later. No UI for it is built
now.

**Why.** Under D3 a person chooses the picture, so a backfill cannot be a silent migration. And the
deployment holds no data anyone keeps, so there is nothing to backfill for. The owner wants editing
supported, so the operation is needed anyway; building it once serves editing, and any later
"find a picture for this old recipe" flow.

Rejected:
- **A backfill that picks automatically from source snapshots.** Contradicts D3's choice.
- **A "find pictures" flow for old recipes now.** Not needed while the data is disposable; the D4
  operation is what it would be built on.

**Obligations:**

1. The demo is reseeded after the feature ships, through `seed.py` and the import path, so its
   recipes go through the D3 choice. The operation's shape is open item 7.

## D5 — The set-picture operation takes an upload, re-finds snapshot candidates, or removes; the picture is outside the revision history

**Decided.** The D4 operation does three things:

- **Upload** a file as the recipe's picture, through the same path and limits as avatars
  (`internal/media` decode, 5 MB, the ADR-00033 refusals), processed with D1's fit-without-crop.
- **Re-find candidates** by running D3's candidate step against the recipe's saved `html` source
  snapshot, returning the options for a choice. This is how a person gets back the candidates D3
  discarded.
- **Remove** the picture, returning the recipe to the placeholder.

Setting a picture from a typed URL is not included.

The picture's key is **not part of the recipe document**. It is a column (or row) written by
statements of its own, like `video.sql` and `recipe_fields.sql`. A picture change is not a
revision, and restoring a revision leaves the picture as it is. Replacing a picture deletes the
previous object.

**Why.** Upload is the only route for a recipe with no source page; re-find covers "I picked the
wrong one" without keeping D3's discarded candidates; remove is the placeholder. Outside the
document, because inside it every restore of an older revision would point at an object deleted
when the picture was replaced, unless no picture were ever deleted. The picture is decoration
(D1), so it is not worth either.

Rejected:
- **From a typed URL.** More fetches of an arbitrary URL while open item 6 is unresolved, for a case
  upload already covers. Can be added later.
- **The picture key inside the recipe document, like the video ID.** Broken restores, or keeping
  every old picture forever.

**Obligations:**

1. The store keeps the picture key outside the recipe document; a test restores an older revision
   and shows the picture unchanged.
2. Tests cover upload (including a refused file), re-find against a saved snapshot, remove, and that
   replacing a picture deletes the previous object.
3. Re-find on a recipe with no `html` snapshot returns no candidates, not an error.

## D6 — Agents cannot set a recipe's picture over MCP, for now

**Decided.** The D5 operation is not exposed as an MCP tool. An agent's draft still gets D3's
candidates and waits for the person's choice (D3 obligation 4); that is the only way an agent's
work leads to a picture.

**Why.** AGENTS.md lets nothing an agent produces change a recipe without review (ADR-00034,
ADR-00035), so an MCP set-picture tool would have to be a proposal the person accepts, and
proposals carry no media today. Nobody has asked for it, and the review path for media is the
expensive part.

Rejected:
- **An MCP tool that proposes a picture for review.** Deferred, not refused: it needs proposals that
  carry media, which is its own decision when an agent use case appears.

**Obligations:**

1. No MCP tool sets, changes or removes a recipe's picture.

## D7 — Images are built now as domain functions; the discrete API is decided separately

**Decided.** The owner wants the work behind the web handlers split into a discrete API, exposed by
the same binary for now. That is decided in its own proposal and ADR, not here, and the image work
does not wait for it. The image work keeps its logic out of handlers: candidate finding, fetching,
processing, choosing, setting and removing are functions in domain packages (`ingest`, `media`,
`store` or a new package named for what it provides), and the handlers only parse the request,
call them and render.

**Why.** The API question is wider than images: its boundary (in-process Go interfaces or
HTTP/JSON), its authentication (session cookie, ADR-00030, or API keys), whether MCP becomes a
client of it, and how it relates to ADR-00012. Building images as domain functions means they move
behind any API boundary without being rewritten.

Rejected:
- **Decide the API first.** Blocks the image work on a larger decision it does not depend on.

**Obligations:**

1. `docs/proposals/discrete-api.md` records the owner's request and the open questions, for its own
   `/overseer:decide` session.
2. Review of the image work checks that no handler under `internal/web/` contains candidate,
   fetch, processing or storage logic.

## D8 — One guarded fetcher for every URL gobbler fetches on someone's behalf: public addresses only, redirects checked

**Decided.** The OAuth metadata fetcher's address guard moves out of `oauth` into a small package
named for what it provides, and three fetches use it: the OAuth metadata document (unchanged
behaviour), the recipe page fetch, and D3's image candidate fetch. The guard:

- refuses to connect to any address that is not public unicast, as `blockedAddr` does today,
  checked at dial time after DNS resolution, so a public name that resolves to a private address is
  refused;
- ignores proxy environment variables, so the guard sees the address actually dialled;
- protects redirects: each hop must be `http` or `https`, each hop's connection passes the same
  guard, and at most 5 redirects are followed (real recipe sites need one or two, for `http` to
  `https` or a `www` change). The OAuth fetch keeps refusing redirects altogether.

A refused page fetch is an `ErrFetch` with a message the importer can read. A refused image
candidate is logged and dropped (D3).

**Why.** D3 has the server fetch URLs the page's author chose, from a server on the owner's home
network: without a guard a hostile page can make gobbler request LAN devices or the metadata
address, and anything that decodes as an image would be shown and saved. The page fetch has the
same hole through redirects, since a URL a member typed can redirect to `192.168.x`. One guard
for all three is the existing prior art generalised, not a second fetch policy.

Rejected:
- **Guard the image fetch only.** Leaves the page fetch's redirect hole, and two fetch policies.
- **No guard; rely on candidates having to decode.** The request still reaches the LAN.
- **Refuse all redirects, as OAuth does.** Breaks ordinary recipe imports that redirect once.

Cost accepted: a recipe on a server on the owner's own network can no longer be imported by URL.

**Obligations:**

1. The guard lives in one package; `oauth`, `ingest`'s page fetch and the image fetch use it, and no
   copy of `blockedAddr` remains in `oauth`.
2. Tests: a name resolving to a private address is refused for the page fetch and the image fetch;
   a redirect from a public address to a private one is refused; a sixth redirect is refused; a
   redirect to a non-`http(s)` scheme is refused; the OAuth fetch still refuses any redirect.
3. The page-fetch change lands in its own commit, as the broken-window repair it is.
4. `docs/operations.md` says that gobbler's outbound fetches ignore `HTTP_PROXY`/`HTTPS_PROXY` and
   never reach private addresses, so recipes cannot be imported from hosts on the local network.

## D9 — The picture shows on library cards and, kept short, in the recipe page header

**Decided.** The picture shows:

- **Library and search cards** (`/`, `/recipes`): full-bleed at the top, fading into the card where
  the text starts (the owner's request).
- **Recipe page header** (`/recipes/{id}`): short, with the title over the fade rather than below the
  picture, so the ingredients stay high on a tablet.

It does not show in cook mode, the home side panels (suggestion, recently made, haven't made in a
while), shopping lists or the journal and history subpages. The review screen shows D3's candidate
picker instead. Exact heights belong to the mockup (open item 5).

**Why.** A picture on the card that vanishes when the recipe is opened reads as broken, so the page
header carries it too. The design is for a tablet on the counter first, so the header stays short
and cook mode gives its space to the steps. The side panels' cards are small and the suggestion card
already holds a mascot head; a picture each would crowd them.

Rejected:
- **Library cards only.** The card and the page it opens stop looking like one thing.
- **Everywhere a recipe appears.** Crowds the side panels and spends cook mode's space.

**Obligations:**

1. `docs/ui/design.md` gains the picture's rules: where it shows, the fade, and the placeholder,
   matching the approved mockup.
2. e2e tests show the picture on a library card and the recipe page for a recipe that has one, the
   placeholder for one that does not, and no picture in cook mode.

## D10 — A recipe with no picture shows a placeholder band, its variant chosen from the recipe id

**Decided.** A recipe without a picture shows a placeholder band in the picture's place, at the same
height, on library cards and the recipe page header (D9), so the grid lines up. The band is drawn
from the tokens and carries the recipe's meal-type picture (`MealIcon`). There is a set of band
variants (tints or patterns, drawn in the mockup), and a recipe's variant is `recipe id mod N`:
the same recipe always shows the same band, and recipes imported one after another differ. The
faded card, header and placeholder set are drawn in `docs/ui/mockup.html` and approved by the owner
before they are built (lessons.md, "An approved mockup is the visual target").

A recipe whose picture object is missing from the store (ADR-00033 accepts losing media with the
storage server) shows its placeholder band instead of a broken image, and the missing object is
logged.

**Why.** The library grid is the main screen, and a mix of cards with and without pictures is
ragged without a band of equal height. The meal-type picture says something true about the recipe.
Variants keep a library with many placeholders from looking like one picture repeated; choosing by
recipe id keeps a card stable across page loads, cacheable, and testable.

Rejected:
- **No band; cards without pictures start with text.** A ragged grid.
- **The mascot as the placeholder.** Repetitive in a grid, and the mascot is never clipped
  (`docs/ui/design.md`), which a band cropped to card height would do.
- **A variant chosen at random per page load.** The owner offered it alongside the hash. It makes
  a card change on every visit and screenshots unrepeatable; a hash gives the same variety, stable.

**Obligations:**

1. The mockup adds the faded library card, the recipe page header with the title over the fade,
   the D3 candidate picker on the review screen, and the placeholder variants, and the owner
   approves it before any UI node is dispatched.
2. A unit test shows the variant is a pure function of the recipe id; an e2e test shows a recipe
   whose picture object is missing renders its placeholder, not a broken image.
3. `docs/operations.md`'s note that a lost media object shows a broken image is corrected for
   recipe pictures.
