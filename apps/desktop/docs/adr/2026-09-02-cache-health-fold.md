---
title: Cache health is a read-side fold over the records, with paused kinds resolved first
date: 2026-09-02
scope: sidecar
status: Accepted
---

# Cache health is a read-side fold over the records, with paused kinds resolved first

## Context

Neither the cache controller nor the kind controller writes a condition (→ [kind records mirror
the catalog](2026-09-02-kind-records-mirror-the-catalog.md)); the verdict is served by gauges.
The gauges carry values that move while every record under them sits still: a file's size, a row
count, a freshness stamp. A paused kind is forgotten by `kubesync`, so the seam has nothing to say
about it and does not know why it was not asked. A kind that has not answered yet looks, to a
naive fold, like a kind that failed.

## Decision

**The gauges are read-side folds on a cadence.** `Caches().WatchStats` measures the file and the
trigger-maintained counts, re-emitting on the store's ping as well. `Caches().WatchHealth` folds
each live cache. `Caches().WatchSyncStatus(clusterID, cacheID)` expands one cache: the discovery
verdict and a row per mirrored kind with its reason and row count. A cache being collected is
skipped. None emits before its first measurement and none carries a `Bookmark`.

**A paused kind's verdict comes from its record, never from kubesync.** `Paused` is resolved from
`Spec.Paused` ahead of every `GetKindState` call: in `kindVerdict`, in `readSyncStatus`, and at
the top of `readCacheHealth`'s loop as a skip, not a filter applied after. Reading a paused kind's
state would report it unanswered, and one unanswered kind pins the whole cache at `Connecting`. A
paused kind still counts in `TotalKinds` and is tallied in `PausedKinds`, which `sameHealth` must
compare or pausing a kind on an idle healthy cache publishes nothing.

**A kind that has not answered is not an offender.** `GetKindState` reporting false is "nothing
observed yet", so the fold counts it neither as unhealthy nor as proof, and the cache reads
`Connecting` until every kind has spoken. That is also what keeps a clear in progress from reading
as a cache that stopped syncing, so the clear needs no flag of its own. `LastLiveAt` is the oldest
proof across the kinds and absent while any kind has none.

**A verdict comes from the records, not from silence.** The health gauge is latest-value with no
departure frame, so a cache the pass skipped would read as its last verdict, or as no verdict for
a late subscriber. Enumerating the records each pass keeps that honest.

**The sync-status fold answers a cache-level verdict first**: `Paused` when every mirrored kind is
paused (paused kinds are skipped, so a fully paused cache has no offenders and would otherwise read
`Watching`), then `StoreFailed`, because a cache whose file will not open arms nothing, so every
kind reads as unanswered and the loop's default would report a broken cache as still connecting.

## Alternatives considered

- **Store the verdict as a condition on the cache record.** Serves a dead process's answer until
  the passes catch up, and a status write per measurement wakes every dependent.
- **Ask kubesync for a paused kind's state.** The seam does not know the kind is paused, and
  teaching it means relaying the switch onto hundreds of registrations.
- **Count an unanswered kind as unhealthy.** Every cache reads unhealthy while starting.
- **Emit health only on change.** A late subscriber sees nothing until something moves.

## Consequences

Health is correct across restarts, clears, and pauses without any stored state, at the cost of
re-enumerating records per cadence. Any new kind-level state that should not count against a
cache must be resolved before `GetKindState`, in all three places.
