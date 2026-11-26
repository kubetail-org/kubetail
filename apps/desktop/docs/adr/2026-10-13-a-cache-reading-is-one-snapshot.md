---
title: A cache reading is one snapshot
date: 2026-10-13
scope: sidecar
status: Accepted
---

# A cache reading is one snapshot

## Context

`CachedData().ReadActive` hands its reader a `CacheReading`, and the log service resolves a view
through several of its statements: the streams, where each runs, whether a container is starting,
and the pods' UIDs. Each statement ran through `CachedData.QueryOwn` on its own, so each read the
cache as it stood when that statement started
(→ [the sidecar's own mirror reads keep their connections](2026-10-13-the-sidecars-own-mirror-reads-keep-their-connections.md)).

A follow joins those answers. When a pod's container started between the streams statement and the
starting one, the first read it as waiting, with no log to stream, and the second as running, so
not starting. The follow then held a container that neither ran nor started, and ended with
*every container of this view has stopped*. That is the moment a follow exists for, and running the
log tests in parallel hit it about one run in three.

## Decision

**A reading's statements run inside one read transaction.** `kubestore.Store.ReadOwn(ctx, read)`
takes a connection of the file's own-query pool, runs `BEGIN`, hands `read` a `Reading` whose
`Query` runs each statement on that connection through the same wrap and limits as before, then
runs `ROLLBACK` whatever `read` answers. SQLite fixes a deferred transaction's snapshot at its first
read, so every later statement sees the cache as the first did. `cluster.CachedData.ReadOwn`
replaces `QueryOwn`, and `ReadActiveCache` runs the reader inside it, binding each `CacheReading` to
one transaction.

## Alternatives considered

- **Resolve again in the log service before ending a follow.** It closes this one pair of
  statements and leaves every other join a resolve makes (where a stream runs, the pods' UIDs, the
  kinds the cache mirrors) able to read two states.
- **Read everything a resolve needs in one statement.** One statement is one snapshot, but the
  resolve's reads differ in shape, and one statement joining them all would be far harder to read
  and keep right than the transaction that gives the same guarantee.

## Consequences

A reading holds one connection and one snapshot for its whole length: a write committing meanwhile
is seen by the next reading, not this one. The WAL cannot checkpoint past an open snapshot, so a
reading must stay short; the log service's are a handful of bounded statements.

`QueryOwn` is gone, so a caller wanting one statement of its own writes a reading of one statement.
KubeQuery's `Query` is unchanged: a model's statement runs alone, on the pool that keeps nothing.
