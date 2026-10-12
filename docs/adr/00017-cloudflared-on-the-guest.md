# ADR-00017: cloudflared runs on the VM guest; Compose publishes gobbler on loopback only

**Status:** Accepted

**Date:** 2026-10-12

## Context

[ADR-00002](00002-single-binary-go-stack.md), section Packaging, says
`cloudflared` runs alongside gobbler as its own Compose service. The owner
already provisions the VM guest's Cloudflare tunnel with their infrastructure
code. A second tunnel definition in this repository would describe the same
thing twice, and the two would drift. A sidecar also cannot be reached by that
guest process, because Compose only exposed gobbler's port to its own network.

[ADR-00003](00003-cloudflare-access-identity.md) requires that nothing but
the tunnel reaches the app, and that the app verifies the Access JWT on every
request.

Options considered:

- **Keep the sidecar.** Duplicates the tunnel the owner's infrastructure code
  already defines, and puts a tunnel token in this repository's `.env`.
  Rejected.
- **Publish on all interfaces.** Reachable from the network, which ADR-00003
  rejects ("trust the network"). Rejected.
- **Host network mode.** Widens exposure to every host interface for no gain
  over a loopback-bound port. Rejected.
- **Publish on guest loopback only.** The guest's `cloudflared` reaches it; the
  network does not. Chosen.

## Decision

This repository defines no tunnel container. The tunnel runs on the VM guest
and is managed by the owner's infrastructure code. Compose publishes gobbler on
the guest loopback only, at `127.0.0.1:8080`; the guest tunnel's service URL is
`http://127.0.0.1:8080`.

## Consequences

This supersedes only the Packaging bullet of ADR-00002 ("`cloudflared` runs
alongside as its own service"); the rest of ADR-00002 stands. ADR-00003 still
holds: a loopback-only port is not reachable from the network, and the app
still verifies the JWT on every request.

**Obligations:**

1. `compose.yaml` defines no `cloudflared` service.
2. gobbler's only published port in `compose.yaml` binds `127.0.0.1`.
3. `docs/operations.md` names `http://127.0.0.1:8080` as the guest tunnel's
   service URL.
