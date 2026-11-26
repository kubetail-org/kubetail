---
title: The objects watch reads identity, and fetches a body only for the rows it sends
date: 2026-08-29
scope: sidecar
status: Accepted
amended_by: [Stamp every row with its write position, and log deletes](2026-08-30-write-positions-and-the-deletes-log.md)
---

# The objects watch reads identity, and fetches a body only for the rows it sends

## Context

The cached-data read loop pings, re-reads the whole collection and diffs it by key
(→ [ADR](2026-08-26-cached-data-read-loop.md)). That ADR's own consequences named the cost it
left behind: `Store.Objects` selected `raw_json`, so a watch on a 10k-object kind held every
compressed body in Go memory for the subscription's life and pulled all of them back out of
SQLite every 250ms to learn that three rows moved.

Nothing in the diff reads a body. The key is the uid and the compare is the resourceVersion. A
body is needed only by the rows that become `Added` or `Modified` frames.

## Decision

**`Objects` serves identity; `ObjectBody(uid)` serves a body.** `ObjectRow` loses its
`CompressedJSON` field, which makes it comparable, and the frame builder fetches by uid for the
rows that become frames. `prev` holds the same rows at identity size, and the snapshot's transient
peak falls with it.

**The frame hook widens rather than gaining an optional `hydrate` beside it.** `frame` takes
`(context.Context, *kubestore.Store, DeltaFrameType, T)`; kinds and events ignore both new
parameters. An optional hook would be a nil branch on every row, for two watches that would never
set it.

**A `Deleted` frame carries no body.** Its `object` stays non-null — the client keys the removal
off the uid, and a change with no entity is dropped rather than folded — but `applyChange` deletes
by id and never looks at the entity it was handed.

**A body that will not load is a null field, never a failed watch.** That was already the policy
for a decompression failure, and it now also covers the row being deleted between the diff read
and the fetch, where the next resync's `Deleted` frame is the real answer.

**The diff's `changed` hook goes and the constraint becomes `comparable`.** The body was the only
reason a hook existed; `a != b` is exactly right, because the server moves the resourceVersion on
every write and a relist rewriting a row unchanged compares equal and sends nothing.

## Alternatives considered

**Keeping one query.** What the 2026-08-26 ADR chose, declining this split as unmeasured. Two
queries against a moving table is its objection, and the whole of it is this: the diff read
observes resourceVersion N and the fetch returns the body at N+1, so a frame can carry identity
from one version and a body from another. Nothing observable is inconsistent — name, namespace and
kind are immutable per uid — and it converges, because the write that moved the row pinged the bus
and the next burst diffs N+1 against a `prev` holding N. That the ping survives a re-read already
in progress is the loop's subscribe-before-read invariant, which the fetch sits inside.

**An optional `hydrate` hook.** See above: a nil branch on the hot path for two watches that would
never set it.

**A covering index** on `(api_version, kind, uid, resource_version)`, making the diff read
index-only rather than one row fetch per uid. Declined: `resource_version` moves on every write, so
the index would be rewritten on the hottest table's hottest path to save page fetches on a read
that runs at most four times a second per watch — and those fetches skip the body anyway, since
SQLite keeps a large blob in overflow pages the scan never touches. It also needs a migration,
which nothing else in this change does.

## Consequences

**The snapshot turns one scan into N point lookups** on the uid primary key — the one cost this
adds, paid once per connection rather than per burst. A chunked `WHERE uid IN (…)` is the escape
hatch if a large kind's first paint regresses; not worth the chunking logic on a guess.

**`Objects` (`ObjectsWithCursor` since the write positions landed) must stay bodiless.** A caller that wants whole rows gets its own query rather than
putting `raw_json` back on this one, which would silently restore the cost for every watch.

**A resync is cheap but still O(collection) in uids scanned.** Going to O(changes) means an
append-only write log, which is a different order of work — see the `object_writes` item in
`docs/TODO.md`.

## Revisit when

A large kind's first paint regresses on the N point lookups, or a second reader genuinely needs
whole rows.
