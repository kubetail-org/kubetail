---
title: A cache's sync is armed by a record's pass, never by a reader
date: 2026-08-28
scope: sidecar
status: Accepted
---

# A cache's sync is armed by a record's pass, never by a reader

## Context

Something has to decide which of a cluster's kinds get mirrored into its cache, and when. Two
answers were available.

**Demand.** A view opens the Deployments table, so `deployments` starts syncing; nothing reads
`leases`, so nothing lists them. It is the obvious answer for a desktop app, and it is what most
caching layers do.

**Policy.** A user enables sync on a cluster; every kind that cluster serves is mirrored, and stays
mirrored whether or not anyone is looking.

The pause switch already existed on the cluster record, and `kubesync` already had a two-level
arming seam — `TrackDiscovery`/`ForgetDiscovery` for a cache, `TrackKind`/`ForgetKind` for a kind.
What was undecided was who calls them.

## Decision

**Arming is policy, never interest.** A kind syncs because a record's pass armed it. Nothing a
reader does starts a sync, and nothing a reader stops doing ends one.

The switch relays down one level and one level only. `clusterCacheController` computes
`cacheSyncEnabled` — the cluster's two toggles, plus whether this cache still mirrors the identity
the cluster is probed at — and calls `TrackDiscovery` or `ForgetDiscovery` with it.
`clusterCachedKindController` calls `TrackKind` off its own spec and never reads the switch at all.

**The two levels AND rather than nest**, which is what makes that possible. A kind's registration
outlives its cache being forgotten: kubesync holds it and runs nothing while the cache is unarmed.
So pausing a cluster is one call, resuming is one call, and neither writes a record or requeues
one — where gating through the records would mean relaying the switch onto hundreds of them, or
walking two owner hops to the cluster on every kind's pass.

**A cache mirrors every kind its cluster serves.** No curated set. Events are one kind among them;
the only thing special about them is which table `kubestore` writes them to.

## Consequences

**A restart resumes without a reader.** The passes run at startup (`WithStartupFullPass`), so every
enabled cluster's caches are filling before any window opens. Under demand-driven arming the first
table a user opened would cold-list while they watched it.

**A view over a cache is a database read, not a subscription to work.** `CachedData()` reads rows;
it never causes any. That is the whole reason the read path could be built and tested against an
empty store months before anything filled one.

**The cost is real and accepted**: a cache holds every served kind, so an enabled cluster with six
hundred CRDs syncs six hundred collections. The bound is on *starts* rather than on runs — the
supervisor's start cap, eight cold lists at a time however many streams are already up (→ [ADR:
jobs and workers](2026-08-28-jobs-and-workers.md)) — because the cold list is the expensive part
and an idle watch is nearly free. A cache too expensive to hold whole is a later decision with its
own spec, and this does not foreclose it: the sync set is the records', so narrowing it means
writing fewer records, not teaching every reader to ask.

**The cache pass owes a `depends_on` edge onto its cluster**, because the switch it reads lives
there and `ClusterCacheSpec` is identity-only. Owning a child wakes nothing, so without the edge a
paused cluster's cache would keep syncing until something unrelated woke it. This is the one place
in the chain that needs one: every other relay lands in the child's own spec, and a spec write is
already a wake.

**A clear cannot be arranged from outside.** Because kubesync owns the workers and nothing above it
can stop them, `Caches().Clear` and `CachedKinds().Clear` run inside `RunWithCacheSyncStopped` /
`RunWithKindSyncStopped`. The store work stays with the caller, which is what keeps a *paused*
cache — one with no session at all — clearable.
