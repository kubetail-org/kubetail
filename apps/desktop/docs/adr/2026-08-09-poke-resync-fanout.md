---
title: Resync via a fan-out poke broadcaster, not a cascade through stored state
date: 2026-08-09
scope: cross-cutting
status: Accepted
---

# Resync via a fan-out poke broadcaster, not a cascade through stored state

## Context

Long-lived connections (cluster watches, the settings-sync SSE) silently die across machine
sleep, network loss, and process freezes. Recovery should be prompt — not wait out a ~45s
HTTP/2 keepalive or a 30m relist. The wake signals live in two places: the OS (resume,
network-return — visible to the Tauri host) and wall-clock gaps (visible to the sidecar
itself, covering SIGSTOP/VM pause and headless runs).

## Decision

`internal/services/poke` is a leaf broadcaster with two jobs: a **wall-clock gap detector** (15s tick,
2× gap factor — a larger jump means the process was frozen; fires `Poke(SourceWallClock)`)
and a **fan-out hub** (`gochan/broadcast`). The host drives `Poke(SourceHost)` over gRPC from
its `wake/` supervisor: platform sources (NSWorkspace/SCNetworkReachability, Windows
suspend-resume + connectivity hints, logind/NetworkManager over D-Bus) classified on rising
edges and coalesced by a trailing 3s debounce, so a wake quickly followed by network-return
collapses to one poke. Host-side poke is best-effort — failures logged and swallowed; the
wall-clock detector is the backstop.

Consumers subscribe directly: the kubeconfig service re-reads the file
(`internal/services/kubeconfig/kubeconfig.go`), `services/cluster`'s `restartSyncsOnResume` restarts every
sync in place through `kubesync`'s `RestartAll` (each resumes cheaply from its persisted
resourceVersion), and the settings engine (`internal/services/cloud/prefsync/engine.go`) reconnects. **A poke is a fan-out,
not a cascade**: an ephemeral "re-establish now" delivered to all consumers in parallel,
deliberately not routed through a durable spec counter or through connection-status
conditions.

## Alternatives considered

**Cascading through conditions** (poke → probe → condition transition → dependents wake).
Rejected for a load-bearing reason: a clean resume produces *no* condition transition — the
connection probes fine — so the cascade would silently fail to restart the now-stale watches
that are the whole point.

**A durable spec counter (`resyncRequestedAt`).** Rejected: pokes are ephemeral process
events; persisting them writes every object on every wake and re-fires on restart, when the
startup pass already covers recovery.

**OS wake detection in the sidecar.** Rejected: it triples the sidecar's platform surface
(the host already has native event loops on all three platforms), and still wouldn't cover
freeze cases — which the wall-clock detector does portably.

**Faster keepalives instead of pokes.** Complementary, not sufficient:
`configureHTTP2Keepalive` (`kubeconn`) tightens detection to ~15s, but that's still detection-by-decay;
a poke resyncs the moment the machine is back.

## Consequences

One broadcaster, three consumer classes, no persistence. Conditions remain the *output* the
webview reads, never the poke transport. New long-lived-connection owners should subscribe to
the bus rather than invent their own wake source. Shutdown order matters: poke's hub closes
last, after subscribers drain (`internal/app` enforces this in its stop chain).
