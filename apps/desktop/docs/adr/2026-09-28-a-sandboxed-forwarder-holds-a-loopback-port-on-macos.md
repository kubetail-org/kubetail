---
title: A sandboxed forwarder holds a loopback port on macOS
date: 2026-09-28
scope: sidecar
status: Accepted
amended_by: [Offer no sandbox on native Windows; WSL2 runs the Linux one](2026-09-28-native-windows-has-no-sandbox.md)
---

# A sandboxed forwarder holds a loopback port on macOS

## Context

A sandboxed command reaches the chat's cluster through `kubeproxy`, served on the run's Unix
socket. kubectl speaks to a `proxy-url` over TCP, so something inside the sandbox must listen on a
TCP port and relay to the socket: the forwarder, `kubetail-sidecar sandbox-init`. On Linux the run
has a network namespace of its own, so its loopback is the run's alone. Seatbelt has no private
loopback: a port the forwarder takes on macOS is on the host's loopback, where any local process
can dial it. The sidecar itself listens on no TCP port. Decided on 2026-09-27; Windows was to get the
same door in its own sandbox runner.

## Decision

On macOS the forwarder listens on `127.0.0.1:<port>`, a free port `Sandbox.Port` picks, for the
run's life, and the Seatbelt profile lets the run reach that port over TCP on IPv4 alone. The port
reaches only the proxy, which answers 401 without the run's token, sent as proxy credentials, and
the kubeconfig's `Host`. The grant dies with the run.

## Alternatives considered

**A Unix socket kubectl dials directly.** kubectl has no `proxy-url` over a Unix socket, and a
wrapper around kubectl would not cover helm, flux or a script's client.

**The sidecar listens on the port.** One listener for every run would outlive each run and widen
the sidecar's surface; the sidecar keeps no TCP listener.

## Consequences

- Any local process can connect to the port while a run lives. It gets 401 without the token.
- A process a sandboxed command spawns out of its group outlives the run and can reach whatever
  later listens on the port ([ADR](2026-09-28-a-macos-run-keeps-its-group-by-refusing-setsid.md)).
- The sidecar's "No TCP" invariant holds for the sidecar's own process alone.
