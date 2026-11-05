# Open decisions: recipe images from the source page

Owner request, 2026-11-05: import a recipe's main image (OpenGraph or structured data) and show it
full-bleed at the top of the recipe card, fading out where the text starts. This reverses ADR-00011's
"recipe photos stay out" and needs an ADR. Being worked through with `/overseer:decide`.

## Verified facts

- ADR-00011 keeps recipe photos out (`docs/adr/00011-v1-scope-and-deferrals.md:43-46`); ADR-00021
  says a thumbnail "is not precedent for photos" (`docs/adr/00021-youtube-video-on-a-recipe.md:87`).
- The structured-data image is already parsed into `extract.Raw.ImageURL`
  (`internal/ingest/extract/extract.go:97`, `jsonld.go:199`, `microdata.go:99`); nothing outside
  `extract` uses it. `og:image` is not parsed anywhere.
- `internal/media` decodes JPEG, PNG, GIF, WebP, HEIC and AVIF and encodes WebP in pure Go
  (`image.go:16`, `:55`), at most `MaxSide = 1024` (`:27`). `Avatar()` crops square (`:150`); a
  fetched image would need a fit-without-crop step.
- URL imports keep the page HTML in `source_snapshots` (kind `html`, `content BLOB`;
  `internal/db/migrations/00027_recipe_proposals.sql:60-70`), so a backfill could read image URLs
  from saved pages.

## Open

1. **Storage.** (a) own copy in object storage via `internal/media` (ADR-00033 store), (b) SQLite
   blob like thumbnails (ADR-00033 rejected blobs for photos), (c) hotlink (breaks the AGENTS.md
   no-remote-assets boundary, leaks views to the source). Dispatcher lean: (a). Presented; awaiting
   the owner.
2. **Which image.** Structured-data image first with `og:image` fallback, or `og:image` only.
3. **Existing recipes.** Backfill from source snapshots, or new imports only.
4. **Where shown.** Cards only, or also the recipe page header; an editor control to remove it.
5. **Look.** Mockup of the faded card approved before build (lessons.md, approved mockup rule).

## Decided

(none yet)
