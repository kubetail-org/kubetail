---
title: A kind reads Stale only when its watch stops sending bookmarks
date: 2026-11-03
scope: sidecar
status: Accepted
---

# A kind reads Stale only when its watch stops sending bookmarks

Amends [A quiet watch is reopened from its cookie](2026-09-27-a-quiet-watch-is-reopened-from-its-cookie.md):
it replaces what reads `Stale`.

## Context

`Stale` was a reopen that hung past `staleAfter` (5 minutes), after a quiet window of another
`staleAfter`. So a watch that had stopped delivering looked healthy for up to ten minutes. The
verdict was also often wrong. `kubeconn`'s HTTP/2 pings fail a dead connection's streams in about
15 seconds, so a reopen that hangs over a live connection is a slow server, not stale rows. Local
clusters (minikube) read `Stale` this way, and each one landed as a Warning on the kind's
timeline.

Silence is evidence only from a stream that sends bookmarks. kube-apiserver's watch cache sends
one about every minute (`defaultBookmarkFrequency`), best-effort: it drops one when the watcher's
buffer is full, which happens only while events are flowing. A watch served from etcd sends none:
core `v1/events` by default. Pings do not replace bookmarks. Behind a proxy (Rancher, Teleport, a
cloud auth proxy, `kubectl proxy`) or an aggregated API server, pings prove only the first hop,
and bookmarks are the only end-to-end proof.

## Decision

**`Stale` is a stream that has sent a bookmark and has since gone `bookmarkStaleAfter` (3
minutes) without a frame.** `kindSyncer.bookmarked` records the bookmark and is reset when each
stream opens, like `proved`. `applyDeltas` runs a second timer beside the quiet window, and any
frame resets both and reads `Watching`. Three minutes is about three missed bookmarks in a row.
The value must stay under `staleAfter`: the quiet window's reopen opens a new stream, which
forgets the bookmark.

**A reopen that hangs past the window reads `Resuming`**, the same as a slow resume. A stream
already `Stale` stays `Stale` through it, until a frame arrives.

**The memory is per stream.** Within a run, a new stream opens only after a quiet window, and a
window with no frame is itself evidence that the server stopped bookmarking. Across runs, the
next stream may be served by a different apiserver. So a kind whose bookmarks stop for good reads
`Stale` for at most one window, then `Watching` once the reopen lands. It reads `Stale` again only
after a later bookmark.

`KUBETAIL_FAULTS`' `stalled` fault (`kubeconn/faults_debug.go`) answers every watch with one
bookmark, then silence, so the path shows in a dev run.

## Alternatives considered

- **Read `Stale` on any silence.** This misreads every collection the server serves without
  bookmarks, as `v1/events` did under [A kind sync proves itself by a
  frame](2026-09-02-kind-sync-verdicts.md).
- **Remember on the session, across streams, that a kind bookmarks.** A kind's later streams may
  be served by an apiserver without a watch cache for it. The memory would then mark a healthy
  quiet kind `Stale` every window.
- **Drop `Stale` altogether and trust the pings.** Pings prove only the first hop, so a stall
  behind a proxy would never be seen.
- **A longer `bookmarkStaleAfter`.** It trades a minute of detection for fewer brief warnings. A
  lasting fault reads `Stale` either way, through the hung reopen.

## Consequences

A healthy watch-cached kind never reads `Stale`, since its bookmarks keep arriving. A local
cluster's slow reopens read `Resuming`, which is not a Warning. A kind that stops bookmarking
behind a proxy reads `Stale` three minutes in, instead of ten. A kind served without bookmarks
can no longer read `Stale` at all; for it, `LastLiveAt` is the only signal.

## Revisit when

kube-apiserver changes its bookmark frequency, or a supported server bookmarks less often than
every minute.
