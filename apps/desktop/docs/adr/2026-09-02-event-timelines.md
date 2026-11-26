---
title: Three event timelines, written unconditionally and read by id alone
date: 2026-09-02
scope: sidecar
status: Accepted
---

# Three event timelines, written unconditionally and read by id alone

## Context

Beehive keeps an event log per `(ObjectID, category)` and bounds retention on that axis
(`maxEventRuns`). Repeating a run's `(Category, Type, Reason)` extends the run rather than
appending. The cluster subsystem has three kinds with something to report and one GraphQL surface
for all of them.

## Decision

**Three timelines, each an `(ObjectID, category)` pair**: `Cluster`/`connection` for reachability
and identity, `ClusterCache`/`discovery` for sweep verdicts, `ClusterCachedKind`/`sync` for one
kind's transitions. Every pass writes unconditionally, so a flapping kind costs one row per
transition and a settled one costs nothing. A session suspended for `NoConnection` writes no
discovery event: that fact is the cluster's, already on its own timeline.

**One read path for all three** (`events.go`): `ListEvents` and `WatchEvents` take an `ObjectID`
and hand it to `clusterClient`. Beehive reads a timeline by id alone, so the client's kind picks
only which registration is checked.

- **A nil `category` adds no option.** `WithEventCategory("")` selects the default timeline, which
  is a timeline of its own; every write here carries a category, so the empty string would answer
  nothing. A nil `limit` is unbounded, since retention already caps each pair.
- **`WatchEvents` is `EventFrameRun` frames, one `EventFrameBookmark`, then the tail**, snapshot
  newest-first because the client upserts by `Event.ID`. The bookmark lands even for an empty
  timeline. The `beehive.WatchEvents` call is synchronous, ahead of `NewStream`, so a refused
  subscribe is an error the resolver answers with rather than a terminal frame.
- **`terminalErr` drops `beehive.ErrNotFound` and forwards the rest.** A record collected under a
  live watch takes its log with it, so beehive ends the stream `ErrNotFound`, but the deletion is
  the answer, and forwarding it would raise `watchFailed` once per open kind timeline when a user
  clears a cache. `ErrWatchTooOld` stays reported. An id that never held a row does not fail; it
  bookmarks an empty snapshot and waits.

`Event.id` reuses the `ObjectID` scalar for its wire form only. Event runs come from beehive's
`EventID` sequence, so an event id is unique within one timeline and must never be handed where an
object id is expected.

## Alternatives considered

- **Write an event only on a changed reason.** Beehive's run extension already makes a repeat
  free, and a guard in the pass drifts from what the pass writes.
- **One method per kind.** Three copies of one method and an unanswerable question about the
  fourth family, which has no timeline.
- **Forward `ErrNotFound`.** A cache clear becomes N watch failures on the client.

## Consequences

Adding a timeline is a category constant and a write in the pass. The empty-category and
event-id traps are the two a new caller hits, and both are stated on the read path.
