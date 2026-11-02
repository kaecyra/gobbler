# ADR-00026: CI publishes the production image to a public GHCR package; the VM pulls it

**Status:** Accepted

**Date:** 2026-11-02

## Context

The `image` job in CI already builds the production image and checks its
contents ([ADR-00013](00013-playwright-e2e-in-node.md): one Go binary plus
tesseract, nothing from `e2e/`). Nothing publishes it. Compose builds the image
on the VM from a `git pull`, so the VM needs git and a build, and it runs
whatever its own build produced rather than the artefact CI checked. A rollback
means checking out an old commit and rebuilding.

The image holds no secrets: every setting reaches the container from the
environment ([ADR-00002](00002-single-binary-go-stack.md)). Publishing it
exposes the code, not the data.

Options considered:

- **Keep building on the VM.** Works, and needs no registry credential. Rejected
  because the VM runs an image CI never saw, and rollback is a rebuild.
- **A private GHCR package.** Rejected by the owner. It would put a pull token
  on the VM, a credential that lives outside the repository for no benefit when
  the image contains nothing private.
- **Docker Hub or a self-hosted registry.** A second account or a service to
  run, for what `GITHUB_TOKEN` already does for the repository's own packages.
  Rejected.
- **Public GHCR package.** Chosen. The VM pulls anonymously.

## Decision

CI publishes the production image to `ghcr.io/kaecyra/gobbler`, and the package
is public. The VM deploys by pulling a tag, not by building.

- **Trigger:** a push to `main` only, after the checks and the image-contents
  check have passed. A pull request never publishes.
- **Tags:** the contents of `VERSION` (for example `0.79.0`) and the commit SHA.
  There is no `latest`, so a deploy always names the version it runs. The
  `VERSION` tag is never moved once published: a merge to `main` without a
  bump (documentation only, see git-workflow) publishes no new version tag.
- **Credential:** the workflow's own `GITHUB_TOKEN` with `packages: write`,
  granted to the publish job alone. The workflow's default stays read-only.
- **Compose** names the published image, and the version comes from
  `GOBBLER_VERSION` in `.env`, which has no default. The VM upgrades by changing
  that value and running `docker compose pull` then `up -d`. The version is not
  written into `compose.yaml`, because every merged pull request bumps
  `VERSION` and the pin would change on every one.

## Consequences

This amends [ADR-00002](00002-single-binary-go-stack.md), section Packaging,
where Compose builds the image on the host. ADR-00002 carries a note pointing
here.

A package created by a workflow is private until its visibility is changed by
hand in GitHub. That is a one-time owner action, and nothing in the repository
can perform or verify it.

**Obligations:**

1. `.github/workflows/ci.yml` has a publish job that runs only on a push to
   `main`, needs the `check` and `image` jobs, and is the only job granted
   `packages: write`.
2. The published tags are `VERSION` and the commit SHA, and no `latest`.
3. `compose.yaml` references `ghcr.io/kaecyra/gobbler` at
   `${GOBBLER_VERSION}`, fails when it is unset, and no longer builds on the
   host. `.env.example` documents `GOBBLER_VERSION`.
4. `docs/operations.md`, sections Bring it up and Upgrade, use `docker compose
   pull` instead of `--build`, and say that the package must be set public once
   after its first publish.
5. After the first publish, the owner sets the package public and confirms an
   anonymous `docker pull ghcr.io/kaecyra/gobbler:<version>` works.
6. A note on ADR-00002, section Packaging, points here.
