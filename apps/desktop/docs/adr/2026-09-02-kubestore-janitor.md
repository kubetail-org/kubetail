---
title: One janitor per open cache file, gated on the freelist and trimming per kind
date: 2026-09-02
scope: sidecar
status: Accepted
---

# One janitor per open cache file, gated on the freelist and trimming per kind

## Context

A cache file grows two unbounded tables (`status_history`, `deletes`) and, after a relist's prune
or a `ClearKind`, holds free pages that only `incremental_vacuum` hands back. Nothing on the write
path vacuums, because the writer is single and the freelist is biggest right after a relist, when
blocking it hurts most. Readers hold cursors into `deletes` (→ [write positions and the deletes
log](2026-08-30-write-positions-and-the-deletes-log.md)), so trimming it can invalidate a cursor.

## Decision

**One janitor per open file**, started in `openFile` and stopped in `(*file).close`, so its
lifetime is the file's and a `Clear`'s fresh file gets one like any other. It trims
`status_history` past `Retention.StatusHistoryTTL` and `deletes` past `Retention.DeletesTTL`, then
runs a bounded `PRAGMA incremental_vacuum`.

- **Gate on `PRAGMA freelist_count`, never on what the sweep itself deleted.** The writers that
  free pages do not vacuum, so a rows-deleted gate would strand the file at its high-water mark
  and `Stats.DBBytes` would report the worst the cache has ever been.
- **The vacuum is bounded** (`vacuumPagesPerSweep`); a backlog drains over following sweeps. The
  `status_history` delete is one statement over a table that is small by construction.
- **The deletes trim records how far it got, per kind**, in its own transaction, so a reader that
  takes a kind's mark and the entries above it in one transaction cannot be trimmed between the
  two. Per kind because cursors are per kind: one global mark would have a busy kind's deletes push
  every quiet kind's cursor past it within minutes. A mark only ever rises, and the upsert enforces
  it, because `write_seq` is on disk while `updated_at` is forced upward in memory from the wall
  clock, so the two disagree across a reopen. Both the trim and the raise are writes (`DELETE …
  RETURNING`, a `CASE` on the upsert), since a read has no home inside a write transaction. The
  bound is by age, because what it bounds is how stale a cursor may be.
- **Nothing waits under `m.mu`.** The stop is a cancel and the sweep runs on the janitor's own
  context, so it aborts mid-statement. All three exits (`Clear`, `Remove`, `Manager.Close`) hold the
  lock across the close, and a wait there would stall `Stats` behind a vacuum. The first sweep runs
  inside the goroutine, not inline in `openFile`, for the same reason.

`NewManager(dir, Retention{...})` is the whole plumbing. Production passes `DefaultRetention`; a
zero `Interval` runs no janitor, which is what a test about anything else opens with. A zero TTL
keeps its table whole, since a cutoff of now would trim everything and raise every kind's mark to
the head of the log.

## Alternatives considered

- **Vacuum on the write path after a prune.** Blocks the single writer at the worst moment.
- **A global trim mark.** Invalidates quiet kinds' cursors on the busy kind's schedule.
- **A janitor per `Manager`, walking open files.** Misses the reopen mid-`Clear`, and couples the
  sweep's lifetime to the wrong object.
- **Joining the janitor on close.** Deadlocks against `Stats` under the manager lock.

## Consequences

Free space returns over minutes rather than at once, and `Stats.DBBytes` reflects it. A reader
whose cursor falls below the kind's mark must re-snapshot (the read loop already does). A new
table that grows unbounded needs a trim here and a retention field beside the other two.
