---
title: A quiet reopen the server refuses as expired relists in place
date: 2026-09-29
scope: sidecar
status: Accepted
---

# A quiet reopen the server refuses as expired relists in place

Amends [A quiet watch is reopened from its cookie](2026-09-27-a-quiet-watch-is-reopened-from-its-cookie.md):
it replaces how a reopen that fails with `410 Expired` is answered.

## Context

A quiet watch is reopened from its cookie every `staleAfter` window. A collection served without
bookmarks — core events, kept out of the watch cache by default — never moves that cookie while
nothing happens, so it stays at the cold list's or the last event's `resourceVersion`.
kube-apiserver compacts etcd every five minutes by default, the same length as the window, so by
the first reopen the cookie has usually been compacted. The reopen is refused as expired, the run
fails `SyncFailed`, climbs the ladder, and the next run cold-lists. On a quiet cluster that
repeats every window, so the kind reads as failing and restarting when nothing is wrong. The fake cluster never expires a position unless a test says so, which is how
this passed its tests.

## Decision

**A reopen refused as expired relists inside the run.** `kindSyncer.stream` owns the quiet-window
loop. A `410` on a reopen, whether it comes back from the open or on the reopened stream, marks the
kind for relist and runs `open` again: a cold list reported as `Resyncing`, then a watch from the
list's position. The run stays up, so the kind stays `Live`, nothing is backed off, and `Restarts`
does not move. This is how client-go's reflector answers the same refusal.

Only a reopen's refusal is read this way. A `410` on a stream that came straight from a list or a
resume still fails the run, since that is a server refusing the position it just served.

The relist is marked on the session before it runs, so if the list fails, the next run retries the
list rather than resuming from the refused cookie.

## Alternatives considered

- **Advance the cookie with a `LIST` of one item on each quiet window.** A limit-1 list returns the
  collection's current position, but an event created just before the old watch was stopped may
  not have been delivered yet, and resuming past it loses it for good.

## Consequences

On a cluster that compacts faster than its events change, the quiet kind costs one cold list per
window instead of one watch reopen, and reads `Resyncing` while that list runs. The list runs after
`pass.Ready`, so it takes no start slot. Only collections served without bookmarks reach this path;
every other kind's cookie is moved forward by its bookmarks.
