---
title: Stamp every row with its write position, and log deletes
date: 2026-08-30
scope: sidecar
status: Accepted
---

# Stamp every row with its write position, and log deletes

## Context

The cached-data watches (`services/cluster/cacheddatawatch.go`) learn about changes by re-reading a whole
collection on every debounced burst of writes and diffing it by uid. The read serves identity only,
so a row is cheap — but the work is O(collection) per burst however few rows moved, and it is paid
by every open watch on the cache. A namespace with ten thousand Pods costs ten thousand rows read
to discover that one restarted.

The store already had everything needed to answer the question directly except an order. SQLite
gives no usable one: `rowid` moves when a relist rewrites a row, `updated_at` has millisecond
resolution and restarts from the wall clock on reopen, and `resource_version` is the cluster's own
opaque string — neither ours nor comparable.

## Decision

**Every row carries the position of its last effective write.** `cluster_meta` holds a counter;
each write transaction takes one number from it (`nextSeq`) and stamps everything it writes with
that number, so rows committed together share a position. The stamp moves only when the write was
effective — a relist that rewrites a kind unchanged leaves every stamp alone, or a cold list would
read as one change per object.

**A delete leaves an entry in `deletes`** — the uid, its kind, and the same position — because the
row a reader would learn about is gone. Every delete path logs first, in the same transaction and
off the delete's own predicate. A row that leaves a kind without being deleted logs one too, from a
trigger (`objects_identity_change`): a reader takes a kind's rows and its deletes by
`(api_version, kind)`, so a uid rewritten under another identity would otherwise be in neither.

**The janitor bounds the log by age and records how far it got, per kind.** A cursor at or below its
kind's mark has lost deletes it never saw and can no longer be trusted.

**The watch reads what moved past its cursor.** `ObjectChanges`/`EventChanges` return the rows
written above a cursor, the uids deleted above it, the new cursor, and the kind's trim mark, all
from one read transaction. A cursor is a position **and** the Kind it was read under, since both
ranges are keyed by that Kind. The loop applies **deletes before writes** and resumes from the
cursor it was handed, falling back to the full read-and-diff when the cursor is below the mark or
the read answers under a different Kind (a rename, or a plural the catalog no longer carries). The
protocol a client sees is unchanged: snapshot as `Added`, one `Bookmark`, then
`Added`/`Modified`/`Deleted`.

## Alternatives considered

**Carry a change payload on the store's ping bus.** The bus would have to say what was written
rather than that something was. It couples every writer to every reader's needs, needs a payload
per subscriber, and a reader that misses a ping has no way back — where a position is a fact on
disk that any reader can re-derive at any time. The bus stays a coalesced signal for exactly this
reason (→ [ADR: the store's ping bus](2026-08-26-store-change-ping-bus.md)).

**Order by `updated_at`.** It is already on the row and already used for the relist's mark and
sweep. But it is forced upward in memory from the wall clock and restarts on reopen, so two rows
can share a stamp and a later write can carry an earlier one. Good enough to decide "this relist
superseded that row", useless as a cursor.

**Log writes as well as deletes.** It would make one table the whole answer. A write's position is
on its row and the row is there for as long as the object is, so a write is never missed and never
expires; logging it too would store the same facts twice on the hottest path in the store.

**Tombstone rows instead of a separate log** — keep the row with a deleted-at column. Every read in
the store would then have to filter, and the table a hundred kinds share would grow without bound
until a sweep ran. The log is written once per delete and read only by a resuming watch.

**Hand the cursor to the client** as a resumable or paged watch. That is a protocol change: it needs
a file identity on the wire (a `Clear` installs a fresh file whose counter restarts), a per-burst
`Bookmark`, and an `Expired` frame. The cursor stays inside the sidecar, where the watch that owns
it also owns the reconnect.

## Consequences

A burst costs the rows that moved rather than the collection. The full read survives as the
snapshot and as the fallback, so nothing new can go wrong that could not already.

The obligations this creates are all about the stamp meaning what it says:

- **"Unchanged" has to mean unchanged in full.** An empty `resource_version` is equal to itself
  forever, and the upsert rewrites `api_version`/`kind` in the same `SET` list — both ride the
  stamp's condition, or a row keeps a position no reader will look below and is silently invisible.
- **Deletes are applied before writes.** A uid can be in both ranges: `ClearKind` logs a delete per
  row and the restarted sync lists the same objects back above it. The writes range only ever
  returns rows that still exist, so this order lands on the live row; the other drops it, and on a
  quiet kind nothing would send it again.
- **A cursor is only usable under the identity it was taken with.** A CRD whose Kind is renamed
  keeps its plural, and the catalog remaps to the new Kind — so the rows the watch holds and the
  deletes the old Kind's worker logged fall outside both ranges. The full diff had no such notion
  because it re-resolved the plural every time; the cursor has to carry the Kind to keep it.
- **The trim mark only ever rises**, enforced in SQL, because `at` and `seq` disagree across a
  reopen and a later sweep can compute a lower position.
- **The positions fail closed.** An unparseable mark and a missing counter are errors, not zeros:
  zero is the answer that says every cursor is valid and nothing was ever written.
- Two client-visible timings change, neither of which changes what the client ends up holding: an
  event whose `resource_version` moved and nothing else now arrives as a `Modified` (objects already
  behaved this way, and a `Modified` with equal fields folds as a no-op), and a clear-then-relist
  inside one burst is a `Deleted` then an `Added` where the diff emitted nothing.

## Revisit when

A client needs to resume a watch itself, or a collection grows past what one snapshot read can
serve. Both want the cursor on the wire, which is the protocol change this deliberately did not
make — and both build on this log rather than replacing it.
