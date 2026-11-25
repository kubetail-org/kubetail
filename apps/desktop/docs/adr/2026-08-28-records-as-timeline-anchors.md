---
title: A record anchors a timeline; it does not mirror a status
date: 2026-08-28
scope: sidecar
status: Accepted
---

# A record anchors a timeline; it does not mirror a status

## Context

`ClusterCachedKind` is one record per kind a cache mirrors — a hundred-plus per cache. Each one's
sync has a verdict (`Watching`, `Stale`, `SyncFailed`, …) that moves on its own, and a history a
user wants to read when something goes wrong.

The Kubernetes-shaped answer is to write the verdict onto the record as a condition. That is what
the schema originally described, and what a reader expects from a record with a `conditions` field.

## Decision

**A verdict is a gauge; a transition is an event. Neither is a stored condition.**

Neither controller writes one. The live verdict is served by `clusterCacheHealthWatch` (the fold
over a cache) and `clusterCacheSyncStatusWatch` (the expansion of one cache, a row per kind), both
read-side folds over `kubesync`'s getters. The history is served by the beehive event log, and the
record's role is to be the **ObjectID that log hangs off**.

Three timelines, each an `(ObjectID, category)` pair — the axis beehive already bounds retention on:

| Timeline | Category |
| --- | --- |
| `Cluster` | `connection` |
| `ClusterCache` | `discovery` |
| `ClusterCachedKind` | `sync` |

`category` cannot be that axis by itself: it is a fixed vocabulary a UI branches on, not an
identity. A hundred kinds sharing one timeline under a hundred reason prefixes would be a hundred
histories interleaved and bounded together.

## Consequences

**A stored verdict would be wrong across a restart, and that is the deciding argument.** Conditions
here are process-scoped: beehive downgrades a previous process's write to `Unknown` until
re-confirmed, so a stored one is either stale or unknown for the length of the startup sweep. The
gauge has no such gap — it reads what the running process knows, and reports "nothing observed yet"
as an absence rather than as a verdict.

**Storing it would also cost a write storm.** A verdict on the record means every transition is a
spec/status write, waking the record and every watcher, for a value no controller reacts to. Only a
UI reads it. The gauges cost one fold per cadence tick per *subscriber*, and nothing at all when
nobody is looking.

**The record still earns its place twice over.** It is the timeline anchor above, and its pass is
what arms that kind's worker (→ [ADR: arming is
policy](2026-08-28-arming-is-policy-never-interest.md)) — desired state, where `kind_catalog` is
what the cluster serves.

**An event is written every pass, unconditionally.** Beehive extends the current run when a pass
repeats its `(Category, Type, Reason)`, so a flapping kind costs one row per transition and a
settled one costs nothing. No controller has to remember what it last logged.

**A suspended session writes no discovery event.** A cache that cannot reach its server is the
*cluster's* fact, already on the cluster's own timeline; logging it per cache would be the same
news once per cache. It still moves the verdict on the gauge, because a gauge answers "what is
true now" rather than "what changed".

**The health fold needs no special case for a clear.** `GetKindState` reporting false means nothing
has been observed yet, and the fold reads that as still-connecting rather than as a failure — so a
clear in progress, which removes its kinds' subjects for the length of the swap, is covered by the
same rule that covers a cache still starting.
