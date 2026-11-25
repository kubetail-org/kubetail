---
title: The cache store signals with a coalesced ping, not a row delta
date: 2026-08-26
scope: sidecar
status: Accepted
---

# The cache store signals with a coalesced ping, not a row delta

## Context

The `CachedData()` watches stream Kubernetes-style deltas (`Added`/`Modified`/`Deleted`) built from
what the cache holds, and the stats gauge re-measures a cache as it fills. Both need to hear that
the store moved. The writers are `kubesync` workers; the readers must not know that, or every
future writer inherits a fan-out obligation.

The obvious shape is to publish the rows themselves at the transaction boundary: a writer commits,
and each subscriber receives exactly what changed. That buys a delta with no re-read — and owes an
ordering guarantee in exchange, since a subscriber that attaches between a commit and its
publication, or one that snapshots after a delta it also received, folds the same change twice or
misses it.

## Decision

The store carries the signal, and it is a **payload-less coalesced ping per key**
(`gobus/conflate`): `objects/<apiVersion>/<resource>` for a kind's writes, `events` for the event
table's. Writers notify after commit. A reader subscribes *first*, snapshots, and on each ping
re-reads and diffs by UID to produce its frames.

Closing the store closes the bus, which is what ends a live watch when a cache is cleared or the
process shuts down — the failure a `Stream.Err()` carries out to the client.

## Alternatives considered

**Row-level delta fan-out at the transaction boundary.** Rejected: once every read is full current
state, an early or late signal costs one idempotent re-read rather than a wrong frame, so the
ordering problem the transactional broker existed to solve disappears. The served types are already
built for the diff — `ClusterCachedDataObject` keeps `RawJSON` in the struct precisely so an
in-place edit differs across two reads, and its string underlying type keeps the struct comparable.

**A bus keyed only per cache.** An event-storm cluster would wake every object watch in it. Keying
per kind costs one string and makes an unrelated write free.

**A poll backstop beside the ping.** Pull-first is a rule about external sources that can drop
events; this bus is in-process, so a missed ping is a bug to fix rather than an environment to
survive. (The stats gauge does tick, for a different reason: the file's size moves with checkpoints
that write no row and ping nothing.)

## Consequences

A reader pays a full re-read per ping, coalesced — a burst of writes is one wake. That is O(rows in
one kind) per burst, which is why the per-kind counts come from the schema's trigger-maintained
`kind_counts` rather than a scan.

Writers owe exactly one thing: notify after commit, never inside the transaction. A notify inside
would wake a reader that then reads pre-commit state and diffs to nothing, and the next ping would
be the one it needed.

Nothing in a frame comes from the signal, so a reader cannot be given a stale row by a
mis-sequenced publish. The cost is that the store cannot tell a reader *what* changed — every
consumer is a re-read.

## Revisit when

A cache grows large enough that re-reading a hot kind per burst shows up — at which point the fix
is a cheaper read (a version column, a changed-since query), not a delta on the bus.
