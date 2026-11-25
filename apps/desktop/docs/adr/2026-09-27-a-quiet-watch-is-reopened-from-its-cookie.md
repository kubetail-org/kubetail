---
title: A quiet watch is reopened from its cookie, and reads Stale only when the reopen hangs
date: 2026-09-27
scope: sidecar
status: Accepted
amended_by:
  - [A quiet reopen the server refuses as expired relists in place](2026-09-29-an-expired-quiet-reopen-relists-in-place.md)
  - [A kind reads Stale only when its watch stops sending bookmarks](2026-11-03-a-kind-reads-stale-only-when-its-bookmarks-stop.md)
---

# A quiet watch is reopened from its cookie, and reads Stale only when the reopen hangs

## Context

[A kind sync proves itself by a frame](2026-09-02-kind-sync-verdicts.md) made `Stale` mean
`staleAfter` (5 minutes) without a delta or a bookmark, on the premise that bookmarks are how an
idle watch proves itself. Not every collection sends them. kube-apiserver keeps core events out of
its watch cache by default, and a watch served straight from etcd sends no bookmarks. On a quiet
cluster, `v1/events` read `Stale` for most of every hour, flipping back to `Watching` only when the
apiserver rotated the watch. The cluster card listed it under `notWatching`, so the model was told
the cluster's events could not be trusted when nothing was wrong.

## Decision

**A window with no frame ends the stream, not the run.** `applyDeltas` returns `errQuietWindow`,
and `kindSync.sync` reopens the watch from the cookie (`kindSyncer.reopen`) and goes on applying
deltas, and the verdict stays `Watching`.

**The proof is a window stood, not an open.** A position the server no longer serves is refused
on the stream, after `Watch` has returned, so an open proves nothing. A stream with no frame that
stands a whole window stamps `LastLiveAt` at the moment it opened: nothing arrived after the
cookie, so the rows were current then. Each stream proves itself: the window that closed one says
nothing for its replacement, which fails the run like any other if it dies before its first
frame.

**`Stale` is a reopen that takes longer than the window.** `openWatch` takes the reason a slow open
reports: `Stale` for the reopen, `Resuming` for a resume. A wedged connection still reads as not
current, one window later than before.

The reopen stays inside the run, so it is not a supervisor restart and does not move `Restarts`,
which still counts a stream going down and coming back.

## Alternatives considered

- **Keep `Stale` on silence and leave events out of the verdict.** Special-cases one kind and
  still misreads any other collection the server serves without bookmarks.
- **Return from the run and let the floor reopen it.** Same proof, but every quiet kind would add
  a `Restarts` every window, burying the flapping signal that field exists for.
- **Probe with a `LIST` of one item.** Proves the server answers, not that the stream from our
  cookie is intact.

## Consequences

A quiet kind costs one watch reopen per `staleAfter` window. A busy collection never reaches the
window, so it costs nothing. A silence of any length reads `Watching` as long as each reopen
lands; `LastUpdateAt` is what shows how long a kind has been quiet.
