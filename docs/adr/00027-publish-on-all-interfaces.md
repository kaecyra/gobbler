# ADR-00027: Compose publishes gobbler on all interfaces of the VM

**Status:** Accepted

**Date:** 2026-11-02

## Context

[ADR-00017](00017-cloudflared-on-the-guest.md) has Compose publish gobbler on
the guest loopback only, and rejects "publish on all interfaces" because the
app's identity check would then be the only thing between the network and the
app.
[ADR-00003](00003-cloudflare-access-identity.md) lists a published port among
the misconfigurations the app must survive.

The owner's monitoring stack runs on another machine in the same private
network and needs to reach the health check. Loopback cannot serve it. The VM
has one IP, so binding that address and binding every interface expose the same
thing; the specific-address option buys nothing here and breaks if the address
changes.

Options considered:

- **Bind the VM's private address.** The narrowest bind that works. Rejected:
  with one IP it is no narrower than all interfaces, and a changed address
  silently takes gobbler off the network.
- **Join the monitoring stack's Docker network.** Needs the monitor on this VM,
  which it is not. Rejected.
- **A separate metrics port.** There is no metrics endpoint yet, and the health
  check is all the monitor needs today. Rejected as scope; revisit when
  Prometheus-style metrics are wanted.
- **Publish on all interfaces.** Chosen.

## Decision

Compose publishes gobbler's port as `8080:8080`, on every interface of the VM.
`cloudflared` still reaches it at `http://127.0.0.1:8080`, which the same
binding serves. The sign-in session check
([ADR-00030](00030-google-sign-in.md)) remains the app's own guard on every
route except the public paths ([ADR-00029](00029-public-paths.md)), and the
development bypass stays forced off in Compose.

## Consequences

This amends ADR-00017: its Decision and obligation 2 (loopback-only binding) no
longer hold. The rest of ADR-00017 stands, including that the tunnel runs on the
guest and this repository defines no tunnel container. ADR-00017 carries a note
pointing here.

Anything that can reach the VM's address now reaches gobbler without passing
Cloudflare, and the health check answers it unauthenticated. The restriction to
the monitor now belongs to the network: the VM firewall or the cloud security
group. Docker's published ports are not filtered by `ufw` rules unless they are
written for Docker's `DOCKER-USER` chain, so a host firewall rule alone may not
do what it appears to.

**Obligations:**

1. `compose.yaml` publishes `8080:8080` and its comments no longer claim a
   loopback-only binding.
2. `docs/operations.md`, section Deploy, says the port is published on all
   interfaces, that the network must restrict who reaches it, and that `ufw`
   does not filter Docker-published ports by default.
3. `docs/design.md` and `README.md` no longer say "loopback only".
4. A note on ADR-00017's Decision points here.
5. The owner restricts port 8080 to the monitoring host in the VM firewall or
   security group. Nothing in the repository can enforce or verify this.
