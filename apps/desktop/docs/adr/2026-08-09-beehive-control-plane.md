---
title: Model the cluster subsystem as a beehive owner chain with opaque ObjectID identity
date: 2026-08-09
scope: sidecar
status: Accepted
amended_by:
  - [Run cluster discovery as a beehive kind, not a loop beside one](2026-08-18-discovery-as-a-beehive-kind.md)
  - [The Cluster pass folds its claim into two conditions and never writes timing](2026-09-02-cluster-conditions-two-subjects.md)
  - [Kind records mirror the catalog on disk, and Paused is the user's field](2026-09-02-kind-records-mirror-the-catalog.md)
  - [A cluster is a row in app.db, mirrored into a runtime object it is deleted through](2026-09-16-clusters-are-rows-mirrored-into-beehive.md)
---

# Model the cluster subsystem as a beehive owner chain with opaque ObjectID identity

## Context

The sidecar tracks kube-contexts, probes their clusters, and mirrors each reachable cluster
into a local cache — long-running, failure-prone, restart-surviving work with parent/child
lifecycle (delete a cluster, its caches and syncs must go too; a physical cluster migrating to
a new kube-system UID must hand its cache over). This is operator-shaped work, and beehive
(`github.com/amorey/beehive`) is a SQLite-backed, level-triggered controller framework built
for exactly it.

## Decision

Beehive is the control plane, hosting four kinds in an owner chain —
`Cluster → ClusterCache → ClusterCacheGVRDiscovery → ClusterCacheGVRSync` — so deletion
cascades via GC. Each `ClusterCache` carries the `kubetail.io/cache-store` finalizer, `cacheFinalizer`
(deletion blocks until sync children drain and the `.db` file is removed — never orphaned); each sync
child carries `gvrSyncDrainFinalizer` (its worker provably stopped). Everything hides behind
`Service` (`service.go`); callers deal only in domain types.

**Identity is the beehive ObjectID.** `ClusterID` *is* the object id (`AUTOINCREMENT`, so a
delete+recreate is a fresh id with no reuse race) — opaque, source-agnostic, stable for the
record's life. It is deliberately *not* the remote cluster's UID (cross-source dedup, if ever
needed, is a read-side grouping on the probed `Server.UID`) and not the kube-context name.
The source's natural key lives only on the beehive **name** (`kubeconfig/{context}`) — a
reconcile/uniqueness key, never the identity. One shared GraphQL `ObjectID` scalar (decimal
string) carries every kind's id.

**The importer is creation-only and the sole creator of `Cluster` objects.** A departed
context is orphaned (`status.Source.Kubeconfig.IsPresent=false`, written by the core
controller), never deleted — so its id survives a return. A future creator (manual, cloud) is
a new `ClusterSpecSource` variant plus a sibling importer, not a new kind: reintroduce a
separate intake kind only if two sources must dedup into one Cluster.

`WithStartupFullPass(true)` is declared per kind on the four kinds that own process-scoped
state (live connections, sentinels, running workers, in-memory `RequeueAfter` schedules) — a
restart invalidates state beehive's owed pass can't know about, since it was never written.
There is deliberately no periodic full pass: controllers re-arm via `RequeueAfter`, and the
out-of-band triggers cover the rest.

## Alternatives considered

**Hand-rolled goroutine supervision per cluster.** Rejected: reconciliation, backoff,
dependency wake, cascade delete, and crash recovery would each be rebuilt ad hoc; beehive
provides them level-triggered with durable state.

**The kube-context name as identity.** Rejected: renames and returns would change identity or
force fragile mapping; ids must survive a context leaving and returning.

**The remote cluster UID as identity.** Rejected: unknown until first probe (a never-probed
cluster needs an id), and it changes on physical migration — precisely the event the id must
be stable across.

**Per-kind GraphQL id scalars.** Rejected: one `ObjectID` scalar with one marshalling path
serves every kind; per-kind scalars duplicate code for no type-safety the resolvers use.

## Consequences

Restart-safety and cascade-delete come from the framework; the cost is that beehive semantics
(finalizers, owner edges, the settle handshake) are load-bearing and must not be bypassed —
all beehive details stay behind `Service`. Beehive is cluster-only today, so the
service owns its store/instance; if another subsystem ever needs beehive, hoist the store to
`internal/app`.
