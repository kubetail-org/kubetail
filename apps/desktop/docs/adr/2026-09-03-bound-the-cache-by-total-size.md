---
title: Bound the cache by total size, not by per-table event retention
date: 2026-09-03
scope: sidecar
status: Accepted
---

# Bound the cache by total size, not by per-table event retention

## Context

**S-5** in [`security/2026-09-02-threat-model.md`](../security/2026-09-02-threat-model.md) is that
cached events are never aged out: the janitor trims `status_history` and `deletes`, and events are
trimmed in exactly one place — a relist's `Commit` prunes every row the relist did not rewrite. A
healthy watch runs for days between relists, so a cluster that keeps its own events, or produces
them faster than we relist, grows the file with nothing in between.

The finding named events because that is where the gap was provable. But nothing about the risk is
events-shaped. The cache mirrors full object bodies for every discovered kind, and a cluster with a
hundred thousand large objects fills a disk without a single event — no age cap helps there, because
those rows are not stale, they are simply large and legitimately mirrored. Both routes end in the
same place: a cluster an attacker controls chooses how much of the user's disk to consume.

A spec existed for each reading — an `EventsTTL` on `Retention` with a matching write-side cutoff,
and a whole-file ceiling that pauses the sync when the footprint crosses it.

## Decision

**Bound the cache by its total footprint, and do not add per-table event retention.**

The ceiling is built in two halves, both landed. The janitor measures the database plus its
`-wal`/`-shm` sidecars each sweep and publishes an edge-triggered verdict (`kubestore/janitor.go`);
the cache pass acts on it, stopping the sync through `armSync`'s existing switch and reporting
`ReasonSizeLimit` on the health gauge (`services/cluster/caches.go`, → [a stopped cache is held by its
record](2026-09-03-a-stopped-cache-is-held-by-its-record.md)).

The janitor's stance on events is unchanged, and now deliberate at both levels: events retention
stays the server's, mirrored by the relist's prune (→ [one janitor per open cache
file](2026-09-02-kubestore-janitor.md)). There is no `EventsTTL`.

## Alternatives considered

**Age out events by `last_seen`, as originally specced.** It bounds one table and leaves the
unbounded case — object bodies — open, so the ceiling would still be owed afterwards. It is also the
more delicate of the two: the cutoff has to be applied on the way in as well as on the sweep, or a
server whose own retention is longer than ours writes back every row we swept at the next relist,
and a `NULL last_seen` has to be exempt so a malformed event is not silently dropped. That is real
machinery, in the write path, for a partial answer.

**Both.** Defensible, and the reason it lost is ordering rather than merit: the ceiling subsumes the
outcome, so building the narrower mechanism first spends the delicate work before knowing whether
the general one leaves anything worth trimming. If it does, the events TTL is still available — see
*Revisit when*.

**Evict rows to stay under the ceiling.** Rejected, and it is what makes this
decision safe: a cache that silently drops what it was asked to mirror answers questions wrongly,
and a delta watch on top of it never recovers, because it applies deltas to rows it assumes are
whole. Stopping is legible; a truncated mirror is not.

## Consequences

One mechanism bounds every table, including the ones nobody has thought about yet — a new kind, a
new timeline, a future column — because it measures the file rather than its contents.

What it costs is granularity. The ceiling's only response is to stop the whole sync, so a cache
filled by events pauses object syncing too, and the remedy is the user's: `clusterCacheClear`, or a
higher limit. Per-table retention would have trimmed the offending table and kept syncing. We are
accepting that trade, and it is the residual risk this ADR records: **between relists, events still
accumulate without a bound of their own.** The ceiling catches the consequence, coarsely, one sweep
interval late.

The obligation it creates is that the ceiling must actually hold, since it is now the only thing
standing between a hostile cluster and the disk. Its default, its softness, and the tests that pin
it are the whole protection — that is why `security-model.md` carries the ceiling as a row of its
own.

## Revisit when

The ceiling's most common trip reason turns out to be the events table, or the relist cadence
lengthens enough that events dominate a cache between relists. Either makes "pause everything" the
wrong answer to a problem one table caused, and the `EventsTTL` arm comes back — built as the
first alternative above describes it: an age cutoff by `last_seen` on `Retention`, applied on the
write path as well as the sweep, with a `NULL last_seen` exempt.
