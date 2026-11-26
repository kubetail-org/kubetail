---
title: Cached-data watches ping, re-read and diff, and a cleared cache ends one cleanly
date: 2026-08-26
scope: sidecar
status: Accepted
amended_by:
  - [The objects watch reads identity, and fetches a body only for the rows it sends](2026-08-29-object-read-split.md)
  - [Stamp every row with its write position, and log deletes](2026-08-30-write-positions-and-the-deletes-log.md)
---

# Cached-data watches ping, re-read and diff, and a cleared cache ends one cleanly

## Context

The `CachedData()` family is the only one whose reads leave beehive: the nav's kinds and counts, a
kind's objects, and the events window all come off a cache's SQLite file. The write side already
signals with a **coalesced ping** carrying no payload
(→ [ADR](2026-08-26-store-change-ping-bus.md)); this is what the read side does with one.

## Decision

**A watch subscribes, snapshots, and then re-reads and diffs per ping.** Subscribing before reading
closes the only ordering gap: a re-read is always full current state, so an early or late ping
costs one idempotent read rather than a wrong frame. One loop serves all three watches, because the
bookmark discipline and the diff are protocol rules rather than per-watch choices.

**A trailing-edge debounce sits on top of the bus's coalescing, which is not a substitute for it.**
`conflate` merges only what a reader has not yet taken, so a loop that drains promptly gets one
wake per *write* — a rollout on a 5k-Pod cache would turn every delta into a full read and a
full-collection diff. Three constants rather than one: events at 500ms against 250ms for kinds and
objects, since events are the highest-volume stream and the one that storms.

**A failed re-read retries in place rather than ending the stream.** The bus is keyed by what was
written, so a kind nobody writes to may not ping for hours, and one transient error would leave the
client's table empty until something else moved.

**A cache that goes away ends the watch cleanly — `Stream.Err()` nil.** A clear is a user pressing
a button, not a watch breaking. A non-nil `Err` is filed as a watch failure, reaches the client as
`extensions.watchFailed`, and puts an error in front of the user **once per open watch** while
suppressing the transport's backoff reset. The clean path reconnects silently, holds last-known
data, and refills from one snapshot.

**Objects diff on `(uid, resourceVersion)`, not on the whole row.** The server bumps the
resourceVersion on every write, so it says an in-place edit happened without inflating the body:
the read hands `raw_json` back as stored and only a row that becomes a frame is decompressed. The
field is named `CompressedJSON` so a caller that forwards it unchanged is obviously wrong. Kinds
and events have no equivalent column and compare their whole row.

**Only a cache that went away degrades to empty.** `ErrRemoved` and `ErrClosed` mean a clear or a
teardown took it, which a read answers as empty and a watch ends clean. Every other open failure is
a storage fault and is reported — as an error from `ListKinds`, and as `Stream.Err()` on a watch.
The trap is reading one as "no file yet": `WatchOpen` fires only when a file is *created*, so a
cache whose file already exists and will not open would wait for a signal that never arrives, and
the client would sit on an empty table with no reason for it.

**A read claims the cache's file and never creates one**, and the claim is pinned to the file it
opened. Claiming rather than borrowing an already-open file is what makes an idle cache readable:
nothing holds one open — the workers release on pause and on shutdown — so a borrowing read reports
a paused cache as empty over rows still on disk. The pin matters because `Manager.Clear` installs a
fresh empty file on the same entry, so a claim that resolved the entry per call would silently
re-read the replacement and report a `Deleted` for every row the client holds.

## Consequences

**A re-read is cheaper, not cheap.** `Objects` still reads every compressed body off disk on every
fire, and the loop holds the whole compressed collection for the subscription's life, because a
`Deleted` frame carries the departed row's body. Reading keys first and fetching bodies only for
the changed set would fix that; declined for now, because it doubles every read into two queries
against a moving table for a saving we have not measured.

**A watch opened before its cache has a file does not stall.** The `Bookmark` goes out on the empty
snapshot — a cache with nothing synced is empty, not pending — and `Manager.WatchOpen` is what
takes it live when a writer opens one, with the rows arriving as ordinary `Added` frames.

**Reads need their own connection pool.** A watch re-reading on every ping would otherwise queue
behind the single-connection writer.

**A watch keeps its cache's file open for as long as it runs**, which is the cost of making an idle
cache readable. Bounded by open windows rather than by cache count, and `Clear`/`Remove` both work
over a live claim, so the price is descriptors. The loop owes the `Release` and the subscription's
`Close` on every exit.

## Alternatives considered

**Row deltas on the bus.** Rejected on the write side already: it couples the signal to the store's
transactions, and a dropped or reordered delta is a wrong frame rather than a wasted read.

**Rebinding across a clear instead of ending.** `main` did this, and to do it it emitted one
`Deleted` per held row first (`emitEmpty`). That blanks the client's table for the whole gap, where
a dropped watch holds last-known data and refills from one snapshot — which is what the webview's
transport already does.

**Reporting the clear as a watch failure.** It is the mechanism that already exists, and it is
wrong here for the reason `Stream`'s own doc gives for the opposite default: an error exists to
make an *invisible* retry loop visible. The reconnect succeeds, so the next snapshot is the report.
