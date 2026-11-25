---
title: A recorded identity conflict rebuilds the connection, woken by an edge
date: 2026-08-27
scope: sidecar
status: Accepted
amended_by: [The supervisor runs two kinds of thing — jobs and workers](2026-08-28-jobs-and-workers.md)
---

# A recorded identity conflict rebuilds the connection, woken by an edge

## Context

[ADR: connection-carried identity](2026-08-25-connection-carried-identity.md) made a server
replaced behind unchanged credentials *detectable*: a second, different uid read over a
`Connection` records a conflict, and `ConnFor` then refuses every caller. That turned silent
corruption into a visible stall — and left the stall permanent. `connectionProbe.Run` rebuilt only
on a changed credential fingerprint or a missing connection, and a swapped server changes neither,
so identity-scoped work over that connection stopped for the process's life.

## Decision

**The rebuild arm asks the connection.** `connectionProbe.Run` rebuilds when the standing
connection is `conflicted()`, alongside the existing arms. Asked of the connection rather than
compared against `State.Identity()`: that pairing lags a rebuilt connection by a dispatch plus a
round trip, which is the trap the previous ADR exists to close. The conflict was recorded by the
probe that made the request over this connection, so there is one writer and nothing to correlate.
A first identification records no conflict, so nothing rebuilds merely because the probe behind it
answered.

**The wake is an edge, not a level.** Without a wake the rebuild waits out the connection probe's
30s interval. The probe cannot declare a data edge on the identity probes — `resolveLocked` takes
only already-registered names, which is what keeps the probe graph acyclic — so the wake rides
`publish`, the `OnPass` that runs after the pass recording the conflict, and sits **inside the
existing `changed` block**. Recording a conflict empties `news.vouchedFor`, so the edge lands on
exactly the pass that records it.

## Alternatives considered

**Waking on the level — reading `conflicted()` every pass.** Rejected, and this is the one worth
keeping. A `Wake` is a queue add, not a schedule, so nothing paces a condition re-read every pass;
and the conflict outlives the run meant to clear it whenever that run returns *before* the rebuild
arm. A kubeconfig that stops resolving does exactly that: `ReasonResolveFailed` returns early, the
deferred commit sees nothing moved, and the conflicted connection stays in the observable. Every
finished run re-queues its subject's pass, so publish → wake → fail → publish becomes an unbounded
hot loop that bypasses the backoff ladder, rebuilds `RESTConfig`'s TLS material each turn, and
floods every `stateHub` watcher out to the webview. The edge bounds it at the news transitions.

**Retiring without rebuilding.** Does not help on its own: `Conn` hands out a retired connection
too — `Done()` is how a holder hears about one — and nothing would build a replacement, since the
credentials never moved.

**Recording the identity on `connInfo`.** Rejected by the previous ADR and still rejected: it
would have the connection probe read a uid off its own snapshot, making a transient pairing
durable.

## Consequences

A swapped server costs a window instead of a stall. `Retry`/`clusterConnectionRetry` also becomes a
working manual remedy, since the woken connection run now reaches the rebuild arm.

Two obligations:

- **The wake stays inside `changed`.** It reads as a coincidental nesting and simplifies away to a
  level check, which is the loop above. A test pins it (`TestAConflictWhoseFileStoppedResolvingDoesNotSpin`).
- **The trigger stays on the connection.** Anything that reintroduces a comparison against
  `State.ServerUID` reintroduces the stale pairing.

An endpoint flapping between two servers re-records a conflict per flap and rebuilds per flap,
paced by the serverUID probe's cadence. Acceptable, and pathological enough not to design for.

`Conn` still hands out a conflicted connection until the rebuild lands — pre-existing, and
unchanged here. This shortens how long one stands; it does not close that window.

**Recovery takes two signals, so holders got a helper.** `Done()` fires when the old connection is
retired, but the replacement is unstamped for a round trip after that, so `ConnFor` refuses through
the window. `ReadyFor`/`AwaitConnFor` are that wait, as free functions over `Lease` — a probe `Run`
must not use them, since blocking holds an engine worker.

*Both helpers went with the kind supervisor, which left nothing that waits: a run parks at the
`Lease.ConnFor` gate, and the session's one `WatchState` bridge (`wakeOnConnectionChange` in
`kubesync/session.go`) wakes it once the replacement vouches — see the jobs-and-workers ADR.*

## Revisit when

A conflict lingers long enough to need surfacing on the record. While the rebuild is in flight the
stall is sub-minute, so the per-probe observability row (`docs/TODO.md`) is where a lingering one would
show.
