---
title: One SQLite file per cache, behind a refcounted registry
date: 2026-08-26
scope: sidecar
status: Accepted
---

# One SQLite file per cache, behind a refcounted registry

## Context

A `ClusterCache` mirrors one physical cluster — the identity a `kube-system` UID names — and the
sidecar holds as many as the user has clusters, plus one per identity a cluster has migrated
through. Each holds the same shape of data: objects of a hundred kinds, events, and the sync
bookkeeping its workers resume from.

Two things had to be cheap. `ClusterCacheStats.Bytes` is a gauge a user watches while a sync runs,
and `Caches().Clear` has to leave a cache empty and immediately usable. Both are answers about one
cache alone.

The writers (one `kubesync` worker per kind) and the readers (the `CachedData()` family, the stats
gauge) had to reach the same file, and the change bus a read re-reads on is in-memory state on the
handle they share.

## Decision

`internal/kubestore` keeps one SQLite file per cache at `<data-dir>/caches/<cacheID>.db`, with its
own migration sequence (`migrations/0001_init.sql`), and hands it out only through a refcounted
`Manager`: `OpenOrCreate`/`Release`. `Bytes` counts the `-wal`/`-shm` sidecars
alongside the main file, `Clear` is "close the claims' file, delete it, reopen", and `Remove` is the
same without the reopen, tombstoning the id so nothing can ever open that name again.

The manager is the only place the sequencing is expressible, so it owns it: deleting under an open
file does not fail on POSIX, it silently forks the world — the old handle writing to an unlinked
inode while a reopen starts empty. A `Store` is therefore a *claim*: it resolves the file per call
and answers `ErrClosed` once that file is gone, which is what lets a `Clear` swap underneath live
holders and a teardown retire them without handing anyone a closed database.

The manager holds only what is about which file and its life; a cache's contents are reached
through the `Store` you opened. **Every `Store` is a claim**, and `OpenExisting` is the door that
does not create: it claims a cache that already has a file, for a reader or for a caller clearing
a kind. Two paths reach a cache without claiming it, because neither touches its contents:
`Subscribe` borrows the change feed of a file someone else holds open, and `Stats` measures file
size plus the per-kind counts through the open file or a read-only open (`mode=ro`) — so a paused
cache, with no workers and nothing open, still reports what it holds.

## Alternatives considered

**One database for every cache, scoped by a cluster column.** Clearing a cache becomes a delete of
a hundred kinds' rows across every table, holding a writer other caches share; the file never
shrinks without a vacuum; and `Bytes` stops being answerable per cache at all. The isolation is
what makes both operations O(1) file work.

**Bare stores rather than a registry.** The bus lives on the handle, so writers and readers must
share one — and a `Clear` with no registry cannot know which handles to close first. Refcounting
is what makes "the last release closes the file" true without a lifetime rule nobody enforces.

**Stat the main file for `Bytes`.** It swings with checkpoint timing, so the number moves for a
reason no user can see.

## Consequences

A cache's whole life is file operations: create on the first `OpenOrCreate`, delete with the record. That
gives the teardown pass a hard obligation — the file is named for an id that dies with the record,
so a pass that lets it survive orphans it on disk for good, which is why `clusterCacheController`
calls `kubesyncSvc.ForgetCache` (stop and wait) before `kubestoreMgr.Remove`.

Cross-cache queries are not possible. Nothing wants one: every read is scoped to a cache already.

Per-cache files also mean per-cache connection pools. The writer pool is capped at one connection,
so a cache's kinds serialize their writes against each other and against nobody else's.

## Revisit when

A view needs to ask one question across every cluster at once — "where is this image running" —
often enough that fanning it out over N files is the wrong shape.
