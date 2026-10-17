# ADR-00021: A recipe may carry a YouTube video, played only on click, with its thumbnail stored in SQLite

**Status:** Accepted

**Date:** 2026-10-17

## Context

The owner wants to link a recipe to a YouTube video, optionally, and have it
play at the top of the recipe page in a minimal player. Two settled positions
stand in the way:

- [ADR-00002](00002-single-binary-go-stack.md) (static assets, obligation 3)
  and the repository boundary on third-party scripts: no page loads a script
  from a third-party origin. YouTube's embedded player is Google's JavaScript,
  and it tracks; the `youtube-nocookie.com` host defers its storage until
  playback but still loads the player.
- [ADR-00011](00011-v1-scope-and-deferrals.md) ("Out, until decided
  otherwise"): no recipe images, downloaded or uploaded. A video preview needs
  an image.

Options for the player:

- **An iframe on every page that has a video.** Simplest, and the player is
  ready at once. Rejected: Google's code loads on every view of such a recipe,
  whether or not anyone watches, which is the tracking the boundary exists to
  keep out.
- **A link to YouTube, no embed.** Keeps the boundary whole. Rejected: it is
  not what was asked for; the video does not play on the page.
- **A local placeholder that becomes the player when pressed.** Nothing from
  YouTube loads until a person chooses to play the video. Chosen.

Options for the placeholder's thumbnail:

- **Hotlinked from YouTube's image host.** Rejected: a third-party request on
  every view, the same leak the click-to-play exists to avoid.
- **No thumbnail, a plain placeholder.** Rejected by the owner: the preview is
  worth having.
- **Downloaded by the server, kept as a file on the data volume.** Workable,
  but the nightly snapshot ([ADR-00010](00010-backups.md)) copies only the
  database, so backup would grow a second job.
- **Downloaded by the server into an object store (MinIO or similar).**
  Rejected: a new service, credentials and a second backup path, for files of
  tens of kilobytes.
- **Downloaded by the server into a SQLite table.** A YouTube `hqdefault`
  thumbnail is tens of kilobytes; SQLite reads blobs of that size at least as
  fast as the filesystem, and the existing snapshot covers them. Chosen. The
  owner raised table growth: kept in its own table, keyed by video ID, the
  rows are read only by primary key and never touched by recipe queries, and
  one row serves every recipe and revision that names the video.

## Decision

1. **The recipe holds a YouTube video ID, optionally, and nothing else about
   the video.** The editor accepts a pasted YouTube watch, short-link, shorts
   or embed URL, or a bare ID, and stores the 11-character ID; anything else is
   rejected with a message. The ID is part of the recipe revision
   ([ADR-00004](00004-recipe-data-model.md)), so history, restore and a shared
   copy ([ADR-00018](00018-households-as-tenants.md) Sharing) carry it.
2. **Click to play.** The recipe page shows the stored thumbnail, or a plain
   placeholder, with a play button. No request goes to a YouTube origin until
   the button is pressed. Pressing it replaces the placeholder with an iframe
   from `youtube-nocookie.com` that starts playing. Any script that does this
   is embedded in the binary.
3. **Thumbnails are a SQLite table, one row per video ID.** When a revision
   with a video ID is saved and no row exists for that ID, the server downloads
   YouTube's `hqdefault` image and stores it. A failed download is logged, does
   not fail the save, and leaves the placeholder; a later save of a recipe with
   that ID retries.
4. **The thumbnail is served per recipe, behind the household check.** It is
   reached through the recipe it belongs to, never by video ID, so one
   household cannot learn which videos another has stored.
5. **Ingest reads a YouTube video from schema.org Recipe markup** into the
   draft. Like every drafted field it is shown on the review screen and saved
   only through it ([ADR-00006](00006-rule-first-ingestion.md)). A video on any
   other host is ignored.

## Consequences

The no-third-party-scripts boundary now has one exception: the YouTube player,
loaded only by an explicit play action. This amends ADR-00002 (static assets
and obligation 3). Recipe images stay out under ADR-00011, which is amended
for video thumbnails alone; a thumbnail is not precedent for photos, which are
megabytes each and would need their own decision on storage.

**Obligations:**

1. An end-to-end test shows a recipe page with a video makes no request to a
   YouTube origin before the play button is pressed, and loads the
   `youtube-nocookie.com` iframe after (#198).
2. Link parsing is table-tested over each accepted form and a set of rejected
   ones; a store test shows the video ID survives an edit, a restore of an
   earlier revision and a shared copy (#198).
3. Thumbnails live in their own table keyed by video ID; a test with the
   download failing shows the save succeeds and the page renders the
   placeholder (#198).
4. The tenancy spec shows another household's request for a recipe's
   thumbnail returns 404 (#198).
5. Ingest fixtures with a YouTube `video`, a non-YouTube `video` and none
   show the draft carries the ID only in the first case, and it reaches the
   review screen (#199).
6. ADR-00002's static-assets entry and obligation 3, and ADR-00011's
   "Images" entry, carry amendment notes pointing here, and the third-party
   boundary in `AGENTS.md` names this exception.
