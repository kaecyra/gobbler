# ADR-00017: cloudflared runs on the VM guest; Compose publishes gobbler on loopback only

**Status:** Accepted

**Date:** 2026-10-12

## Context

[ADR-00002](00002-single-binary-go-stack.md), section Packaging, says
`cloudflared` runs alongside gobbler as its own Compose service. The owner
already provisions the VM guest's Cloudflare tunnel with their infrastructure
code. A second tunnel definition in this repository would describe the same
thing twice, and the two would drift. Compose also only exposed gobbler's port
to its own network, so gobbler could not be reached by the guest's
`cloudflared`.

[ADR-00003](00003-cloudflare-access-identity.md) says gobbler is reached
through a Cloudflare tunnel with an Access application in front of it. It
rejects trusting the network, and lists "a published port" among the
misconfigurations the app must survive by verifying the Access JWT itself on
every request except the health check.

Options considered:

- **Keep the sidecar.** It gave a self-contained Compose file and a pinned
  `cloudflared` image, so the repository deployed on its own. Rejected because
  it duplicates the tunnel the owner's infrastructure code already defines, and
  a token for a second tunnel would sit in `.env`.
- **Publish on all interfaces.** Reachable from the network, so only the JWT
  check would stand between the network and the app. ADR-00003 treats a
  published port as a misconfiguration to survive, not a design to adopt.
  Rejected.
- **Host network mode.** Drops the container's network isolation, and makes
  the listen address, rather than the port mapping, carry the exposure
  decision: one wrong address in configuration exposes the app on every host
  interface. Rejected.
- **Publish on guest loopback only.** The guest's `cloudflared` reaches it; the
  network does not. Chosen. The cost: the tunnel's version and configuration
  now live outside this repository, and this repository can no longer deploy
  end to end by itself.

## Decision

This repository defines no tunnel container. The tunnel runs on the VM guest
and is managed by the owner's infrastructure code. Compose publishes gobbler on
the guest loopback only, at `127.0.0.1:8080`; the guest tunnel's service URL is
`http://127.0.0.1:8080`.

## Consequences

This amends ADR-00002, section Packaging: its bullet "`cloudflared` runs
alongside as its own service" no longer holds, and ADR-00002 carries a note
pointing here. The rest of ADR-00002 stands, as does ADR-00003.

**Obligations:**

1. `compose.yaml` defines no `cloudflared` service.
2. gobbler's only published port in `compose.yaml` binds `127.0.0.1`.
3. `docs/operations.md` names `http://127.0.0.1:8080` as the guest tunnel's
   service URL.
4. ADR-00002 carries the amendment note at its Packaging bullet, and its Status
   and index row name this ADR.
