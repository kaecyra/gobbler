# ADR-00033: Uploaded media is processed to WebP and kept in S3-compatible object storage

**Status:** Accepted

**Date:** 2026-11-03

## Context

Avatars (ADR-00032) are the first images that people upload. Recipe photos are
likely to follow. Video thumbnails are small, fetched from one known host, and
stored in SQLite (ADR-00021). Uploads are another matter: they come from
people, in whatever format the phone produced, and many of them carry the
location where the photo was taken.

Backups already speak S3 to a configured S3-compatible endpoint (ADR-00010).
The owner plans to run that endpoint on a second machine on the home network.
MinIO, the usual choice, stopped being maintained in 2026.

The image itself has to be checked twice: once in the browser, while the
person crops it, and again on the server, which owns what is stored.

Options considered for where media lives:

- **SQLite blobs, as for thumbnails.** Rejected. Uploads would grow the
  database and every nightly snapshot (ADR-00010). The snapshot is meant to
  hold what cannot be recreated, and media is cheap to upload again.
- **A directory on the VM's disk.** This is kept only as the fallback below.
  A single disk is the one place the owner wants data not to depend on.
- **S3-compatible object storage.** Chosen.

Options considered for the storage server on the home network:

- **MinIO.** Rejected because it is no longer maintained.
- **SeaweedFS.** Mature, but heavier to run than one home machine needs.
- **RustFS.** The closest to MinIO in how it is run, but young, and its
  maturity is disputed.
- **Garage.** Chosen. It is small and built for self-hosting. It lacks object
  versioning, which media does not need.

Options considered for the WebP encoder. Go's standard library decodes WebP
but cannot encode it, and the binary is built without cgo.

- **`chai2010/webp`.** Rejected because it needs cgo.
- **`nativewebp`.** Rejected because it encodes only lossless WebP, which is
  too large for photographs.
- **`linzeyan/webp-go` and `gen2brain/vpx`.** Rejected for now. Both are
  pure Go, but they have the least use and history.
- **`deepteams/webp`.** Chosen. It is pure Go, MIT-licensed, encodes lossy
  WebP and is actively developed. The owner accepted a young library.
- **`gen2brain/webp`.** This is the fallback if `deepteams/webp` fails. It is
  libwebp compiled to WebAssembly and transpiled to Go, so it needs no cgo.

## Decision

### Storage

- Media lives in an S3-compatible bucket. Its bucket is set separately from the
  backup bucket, on the same S3 endpoint and credentials (ADR-00010). The
  application speaks plain S3. The deployment's server is Garage on the home
  network, but any S3-compatible service works.
- Without S3 configured, media is kept in a directory under the data
  directory, behind the same interface. Development, the e2e suite and a
  deployment with no S3 work this way.
- The app serves media itself, through the same admission as the page that
  shows it (ADR-00018). The storage server is never exposed to the internet,
  and no URL points at it.
- Media is not in the nightly SQLite snapshot. If the storage server is lost,
  its media is lost too. For avatars, which can be uploaded again, that loss is
  accepted.

### Upload and processing

- An upload is at most 5 MB. JPEG, PNG, GIF, WebP, HEIC and AVIF are accepted,
  and the server decodes the file to confirm what it is, whatever the browser
  claims. Anything else is refused, with a message the person can act on.
- The person crops the image in the browser with an embedded cropping library,
  pinned like every other frontend asset (AGENTS.md, Boundaries). Cropper.js is
  the expected choice, at a version checked when it is added.
- The browser sends the original file and the crop rectangle. The server
  applies the crop. A format the browser cannot display gets a preview, made
  by the server, to crop against.
- The cropped image is scaled to fit 1024 by 1024 pixels at most, re-encoded as
  lossy WebP, and stored. Re-encoding removes all metadata, including location.
- Processing runs in the background. Until it finishes, the previous avatar or
  the default one is shown. The original upload is kept only until processing
  succeeds. A job the app was restarted in the middle of is retried when it
  starts.

## Consequences

ADR-00021's thumbnails stay in SQLite. This record covers what people upload,
not what the app fetches. The S3 settings of ADR-00010 now also serve media,
plus a setting for the media bucket.

**Obligations:**

1. Media is read and written through one storage interface, with an S3
   implementation and a local-directory implementation. Tests run against the
   local one, and against a fake S3 endpoint.
2. An upload over 5 MB, a file that does not decode, and an unsupported format
   are each refused, and each case is tested.
3. A stored image is WebP, fits within 1024 by 1024, and contains no EXIF, XMP
   or ICC metadata. A test uploads a JPEG with a GPS tag and checks the stored
   file has none.
4. Media is served by the app, after admission. A test shows a member of
   another household cannot fetch a person's media by its address, unless the
   visibility rule allows it (ADR-00032).
5. The cropping library is embedded and its version pinned. No page loads a
   script from another origin.
6. Library versions come from the module proxy and the npm registry when they
   are added, never from memory.
7. `docs/operations.md` describes the media bucket setting, the local fallback,
   setting up Garage as the S3 endpoint for both backups and media, and that
   media is not in the database snapshot.
