---
title: The sidecar's own mirror reads keep their connections
date: 2026-10-13
scope: sidecar
status: Accepted
amended_by:
  - [A cache reading is one snapshot](2026-10-13-a-cache-reading-is-one-snapshot.md)
---

# The sidecar's own mirror reads keep their connections

## Context

KubeQuery runs a statement the model wrote over a cache's views, on a query pool that keeps no
idle connection, so nothing one statement leaves reaches the next
(→ [KubeQuery reads views on a connection of its own](2026-09-27-kubequery-reads-views-on-a-connection-of-its-own.md)).
Opening a query connection runs the driver's hook, which creates every view in
`query_views.sql`.

`CachedData().ReadActive` hands its reader a `CacheReading` whose statements the sidecar wrote: the
log service resolves a view's sources through it, several statements per page of lines. On that
pool every statement paid for a fresh connection and the views' creation. Under the race detector,
which instruments the whole pure-Go SQLite, a page took about 200ms, and the paging property test
ran past the 10-minute limit.

## Decision

**Each cache file has a second query pool for the sidecar's own statements.**
`sqlitepool.OpenOwnQuery` opens it like `OpenQuery` — `mode=ro`, through `kubestore`'s driver, so it
sees the same views and functions — but keeps every connection it opens until the idle timeout, as
`OpenReader` does. `kubestore.Store.QueryOwn` runs a statement on it through the same wrap, limits
and bounds as `Query`. `cluster.CachedData.QueryOwn` exposes it, and `CacheReading.Query` calls it.
KubeQuery still calls `Query`.

## Alternatives considered

- **Keep idle connections on the KubeQuery pool.** That loses the isolation the KubeQuery ADR
  chose. A model's statement can leave nothing a later model statement reads today, and a pool
  shared with the sidecar's statements would make a hostile statement's leftovers a hazard for the
  sidecar's reads too.
- **Read through `readDB` with statements over the tables.** That pool has no views and no
  `body()`, so the log service would read the store's tables, which change as their writers do,
  and lose the views' contract.
- **Resolve a view once per walk instead of per page.** A page request is stateless by design, so
  there is no walk to hold the resolution.

## Consequences

A page's mirror reads share a connection instead of opening one each. A file holds up to two more
connections while it is open.

`QueryOwn` is for text the sidecar wrote. A caller that passes the model's text there runs it on a
connection the next sidecar statement reuses. The name and the interface's doc say so, and nothing
enforces it.
