---
title: The cluster service is one package of record families over private leaves
date: 2026-09-30
scope: sidecar
status: Accepted
---

# The cluster service is one package of record families over private leaves

## Context

The cluster subsystem was first built as `internal/cluster`, split in August into three packages
by role — a boundary, its controllers and a domain of types — behind a `ClusterService` of five
record families (`Clusters`, `Caches`, `Discovery`, `Syncs`, `Data`). Connection probing lived in
the core controller: per-cluster reconcile locks, a sentinel watch on `kube-system` to notice a
lost connection, and an in-process retry bus.

That subsystem was torn down and rebuilt as `internal/services/cluster` from mid-August on. Probing moved
into a connection pool driven by the supervisor (→ [probe engine](2026-08-24-probe-engine.md), [the
connection probe dials /api](2026-08-25-connection-probe-dial.md)). Discovery and sync became what
`kubesync` does to fill a cache, mirrored into one `ClusterCachedKind` record per kind (→ [kind
records mirror the catalog](2026-09-02-kind-records-mirror-the-catalog.md)). A cluster became a
row in `app.db` (→ [clusters are rows](2026-09-16-clusters-are-rows-mirrored-into-beehive.md)).
This ADR records the package shape that rebuild settled on, written down on 2026-09-30.

## Decision

**`sidecar/internal/services/cluster` is one package.** `service.go` specifies the whole API — `Service`
and the four family interfaces — and bootstraps beehive; its package doc is the map. One file per
family holds everything about that kind: its beehive shapes, the record served to GraphQL, its
delta-watch frame, and the controller that writes it (`clusters.go`, `caches.go`,
`cachedkinds.go`, `cacheddata.go`). `shared.go` holds the vocabulary every family reuses. The
schema binds these types 1:1 by name in `gqlgen.yml`.

**Mechanisms are leaves under `services/cluster/internal/`**: `kubeconn` (the connection pool and its
probes), `kubesync` (discovery and the per-kind syncs that fill a cache) and `kubestore` (the
cache's SQLite files). The compiler keeps them private to `services/cluster`. A leaf speaks native
vocabulary — GVRs, a `rest.Config`, cache rows — and never the records above it; the controllers
translate. A leaf that reaches for a record type gets an import cycle, which is the enforcement.
The connection surface is the one exception: it reads no beehive object, so its types alias
straight through (`Lease = kubeconn.Lease`). A controller holds policy only. Mechanism growing in
one is the signal to extract another leaf, and `go test ./internal/services/cluster` staying fast is how
that shows.

**The families are views.** `Clusters()`, `Caches()`, `CachedKinds()` and `CachedData()` return
stateless structs over the one `*service` (`clustersAPI{s}` and siblings), so the split shapes the
API, not the control plane. Methods are VerbNoun, with the noun elided when it equals the family's
subject. The connection surface (`AcquireConnection`, `RetryConnection`) and the kind-agnostic
event reads (`ListEvents`, `WatchEvents`) stay on `Service`. Each family is asserted separately —
`var _ Caches = cachesAPI{}` in `service.go`, and again for the resolver tests' fake in
`graph/cluster_testutils_test.go` — since satisfying `Service` only proves the accessors exist.

Two decisions carry over from the first build's connection probing.

**Client-go's HTTP/2 health check is tightened.** `configureHTTP2Keepalive`
(`kubeconn/connection.go`) sets `HTTP2_READ_IDLE_TIMEOUT_SECONDS=10` and
`HTTP2_PING_TIMEOUT_SECONDS=5`, only where the operator has not set them, so a silently dropped
API-server connection is noticed in about 15s instead of 45s. `kubeconn.New` calls it, because the
vars are read when a transport is built and the pool builds every one.

**A cache exists only for a confirmed identity.** The cluster pass creates a `ClusterCache` only
for a `kube-system` UID its probe read (`ensureCache`, `clusters.go`), and reports whether it could
read one as the `Identified` condition (`shared.go`). An observation keeps its value through a
failed probe, so a transient disconnect never removes a cache or names a new one.

## Alternatives considered

**Three packages by role — boundary, controllers, domain.** This was the first build. It forced
exports for every helper that crossed a line, and a change to one kind touched all three packages.
What it bought was fast boundary tests and a compiler-checked direction. The leaves give both:
the I/O the slow tests paid for lives in a leaf, and the direction that matters — mechanism never
learning the records — is the one the import cycle enforces.

**Discovery and sync as record families of their own.** They were `Discovery()` and `Syncs()`,
over two beehive kinds per GVR. Discovery is now a sweep inside `kubesync` that writes a table, and
one record per kind mirrors that table, so there is one family, `CachedKinds()`.

**Probing inside the cluster controller**, with a sentinel watch, per-cluster locks and a retry
bus. Rejected: it put dialing on a reconcile goroutine, which is I/O in a controller. The pool
owns the probes, and the pass reads what the claim last found (→ [two conditions, no
timing](2026-09-02-cluster-conditions-two-subjects.md)).

## Consequences

A reader opens one file to find everything about a kind. The package's tests stay fast only while
the controllers do no I/O; a new mechanism goes in a leaf. A new family needs its own `var _`
assertion in both places. The connection types are the leaf's, so a change to `kubeconn`'s
exported shape is a change to the boundary.
