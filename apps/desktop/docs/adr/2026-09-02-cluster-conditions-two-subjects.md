---
title: The Cluster pass folds its claim into two conditions and never writes timing
date: 2026-09-02
scope: sidecar
status: Accepted
amended_by:
  - [Connected carries the probe's class of failure as its reason](2026-11-03-connected-carries-the-probes-class-of-failure.md)
---

# The Cluster pass folds its claim into two conditions and never writes timing

## Context

`clusterController.Reconcile` observes the kubeconfig and reads the cluster's `kubeconn` claim for
what connecting with that context's credentials revealed (→ [the connection
probe dials /api](2026-08-25-connection-probe-dial.md)). Every watcher streams the `Cluster` record, and
beehive suppresses a status write only when the bytes are equal, so what the pass writes decides
how often every subscriber is woken. The pass also has to explain a healthy-looking cluster that
never gets a cache.

## Decision

**No pass dials.** The claim reports what its last probe found; dialing stays off every reconcile
goroutine. `clusterResync` re-runs each record's pass on its own timer and is the only thing that
does. Holding the claim is the pass's other job (`ensureLease`/`dropLease`, keyed by `ClusterID`):
holding arms the probe, so a disabled, tombstoned, or non-kubeconfig record is dropped and costs
no dial. A boundary caller takes its own claim, since the pool refcounts and a log tail ending must
not stop the cluster being probed.

**The pass reconciles the claim, then observes.** `reconcileConnection` is the one place that
touches the pool and returns a `connectionFinding`: `observed` (the claim's `*kubeconn.State`, nil
when there is no claim), plus `Connected`'s reason. Nil covers the three findings made before the
pool is involved: switched off, context left the file, credentials will not resolve. `inactive`
marks the first two and takes precedence, since the pool cannot see a choice the user made. The
verdicts are pure functions of that finding, so the claim's lifetime happens once while each
condition reads the same value. A record with no credentials to resolve gets no conditions at all.

**Two conditions with two subjects**: `observeConnected` (did we reach it) and `observeIdentified`
(could these credentials name it, from the `kube-system` UID). Two switches rather than one shared
verdict, because the aspects fail independently. A namespace-scoped user gets `Connected=True`
with `Identified=False/UIDUnreadable`, which is the only thing that explains a cluster that never
gets a cache (`ensureCache` skips a record with no UID). The bar for a condition is a distinct
remedy: `Connected` points at the network, the kubeconfig, or the credentials; `Identified` at an
RBAC grant. The server's own readiness is not one; a lease holder that wants it reads
`State.Readiness`.

`Connected` carries `Inactive`, `Connecting` until a probe lands, or `ProbeFailed` with that
attempt's message. A broken kubeconfig is reported on the record rather than failing the pass,
since beehive's backoff cannot fix a file. The other conditions derive `NoConnection` where a
probe never reached the server.

**`foldState` copies values and keeps them through a failure.** It projects `status.server`
(`uid`, `version`, `endpoint`) and `status.principal` (`username`, sorted `groups`). It decides no
retention of its own: an `Observation` already keeps its last answer, so a probe that has never
answered leaves its field alone, which is what stops a first pass from clearing the UID a live
cache is named for. The record's copy is the durable one; a restart empties the pool's.

**Only the values, never the timing.** `Reason`, `Latency`, `Failures`, and `NextAttempt` stay off
the record because they move every cycle. The record has no timestamp field at all. A reader that
wants them takes a lease. The steady state must be silent: a timestamp in the status, or in a
condition written unconditionally, would re-emit the record on every probe.

## Alternatives considered

- **Dial from the pass with a timeout.** Blocks a beehive worker per unreachable cluster.
- **One combined `Ready` condition.** Cannot say "reachable but cannot identify", which is the
  case users most need explained.
- **A `Ready` condition mirroring server readiness.** Nothing gates on it and no user action
  follows from it.
- **Publish latency and next-attempt on the record.** Re-emits every cluster to every watcher on
  every probe; the schedule gauge (`clusterScheduleWatch`) serves them instead.

## Consequences

A cluster's record settles within one pass of its probe settling and then costs nothing until
something moves. Anything added to `ClusterStatus` inherits the silence rule: it must be a value
that moves only when the fact does. The `clusterScheduleWatch` gauge exists because timing was
kept off the record.
