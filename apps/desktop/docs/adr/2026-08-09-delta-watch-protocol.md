---
title: Stream each kind as its own Kubernetes-style delta watch, joined client-side
date: 2026-08-09
scope: cross-cutting
status: Accepted
amended_by:
  - [A cluster is a row in app.db, mirrored into a runtime object it is deleted through](2026-09-16-clusters-are-rows-mirrored-into-beehive.md)
  - [Name every data access for its family, kind and operation](2026-11-25-name-every-data-access-for-its-family-kind-and-operation.md)
---

# Stream each kind as its own Kubernetes-style delta watch, joined client-side

## Context

The webview renders a live picture of the cluster control plane: Clusters, their
ClusterCaches, the caches' per-GVR discovery and sync children, plus folded sync verdicts.
These kinds form an owner chain in beehive, but they write at wildly different rates — a
per-kind sync child rewrites far more often than its cache changes, and a cache more often
than its cluster. The frontend needs current state on subscribe plus every change after.

## Decision

Each kind streams **independently** as a Kubernetes-style delta watch: an `Added` snapshot
burst, one `Bookmark` closing it, then per-object `Added`/`Modified`/`Deleted`
(`clustersWatch`, `clusterCachesWatch`, and the
cache-scoped `clusterCachedKindsWatch`; the sync verdict rides `clusterCacheHealthWatch`,
which is a gauge rather than a delta watch and carries no `Bookmark`). The sidecar
folds beehive's per-kind `WatchList` into this shape behind `services/cluster`'s family
`Watch`/`WatchList` methods; subscription resolvers emit the current snapshot first, then
deltas (`mapStream` in `graph/util.go` for hub-backed sources).

The **`Bookmark`** is what makes an empty collection distinguishable from one still arriving:
sent exactly once per stream, carrying no entity (the only frame that doesn't — which is why
every change wrapper's entity field is nullable). Until it lands a consumer is in
`watchPhase`'s `connecting`; a client must never render an empty state before it. Kubernetes'
`initial-events-end` bookmark, in this protocol's vocabulary. The cache-data watches
(`CachedData().Watch*`) send theirs after the first successful read — or after the first bind that
finds no open cache, whose initial state is legitimately empty, so a never-synced or paused
cache reports "empty" rather than spinning forever.

The webview keys each stream into an id-keyed map and **joins client-side, down the chain**: a
cache carries `clusterID`, so caches join onto clusters by it (`src/lib/clusters.tsx`), and a
per-kind record carries an `owner { id kind }` ref, so kinds join onto their cache by `owner.id`,
while the verdict gauge keys on its own `cacheID` — it is a read-side projection, not an owned
record. Derivations that depend on two
kinds — notably which cache is *active* (its `spec.serverUid` matching the cluster's
`status.server.uid`) — are the client's, never a stored field.

High-volume streams are scoped so the always-mounted registry never carries them: the
per-kind watch (`clusterCachedKindsWatch`) is cache-scoped (~100 records per cache), subscribed
only while a sync panel row is open.

## Alternatives considered

**Folding children onto their parent** (a `Cluster` with embedded `caches`, or a cache with
embedded per-kind syncs). Rejected: the lower kinds write far more often, so folding upward
re-emits the parent at the child's cadence — a sync child's 30s heartbeat would re-send every
cluster row to every window. This is the load-bearing reason for the shape.

**An "active" flag on the cache record.** Rejected: the parent's probed UID changes with no
cache event, so a per-cache stream can never keep such a flag fresh. Only the client's live
join against the current cluster status is correct.

**Query + poll instead of watches.** Rejected: the data is exactly what Kubernetes-style
watches were designed for, and polling either lags or hammers.

**Returning `(snapshot, channel)` on the wire, as beehive's Go watches do.** The tuple is the
right shape in-process — the sidecar's own pump takes it, and it is what makes the initial-state
boundary explicit and race-free — but a GraphQL subscription has one root field emitting N
payloads of one type, so there is nowhere to put a snapshot that isn't another frame. A
snapshot query paired with a delta subscription would restore the tuple at the cost of the gap
between the two operations, which the `Added` burst exists to close. The `Bookmark` buys the
boundary without reopening it.

**A first frame carrying the whole list, then per-object frames.** Rejected: it restructures
every reducer around a union, and hands a large snapshot over as one frame instead of
streaming it.

**Single merged stream of typed union frames.** Rejected: consumers would demultiplex by kind
anyway, and per-kind subscriptions let urql dedupe one shared operation per interested
component and let the sync-panel streams mount lazily.

## Consequences

Every reducer must fold the `Bookmark` away rather than key it — a keyed one would put a
phantom row in every table — and a consumer that renders an empty state must gate on
`watchPhase`, never on "no data yet". The frontend owns the joins, which means reducers must
also guard **provenance**: a re-subscribe after a cache swap can replay a superseded cache's
retained rows, so cache-keyed streams carry
`cacheID` (objects also `apiVersion`/`resource`) on every frame, and the shared
`useCacheDeltaWatch` (`src/lib/graphql/use-cache-delta-watch.ts`) rejects frames not from the
active key. Do not "simplify" that guard away — it is what keeps a stale cache's rows from
surviving a context switch. Adding a kind means adding a stream, not a field on a parent.
