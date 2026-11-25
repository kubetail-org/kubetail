---
title: The cluster service resolves a cluster
date: 2026-09-27
scope: sidecar
status: Accepted
---

# The cluster service resolves a cluster

## Context

The cluster card and KubeQuery both read one cluster at one identity: the record, the cache that
mirrors the identity the record names, and that cache's contents. A cache read names its cache,
never the identity, so a reader has to read the record again afterwards and try once more when
`Server.UID` moved in between; otherwise it can describe a cache the cluster just stopped being.

`clustercard` did all of it in one reader. It found the active cache with a copy of the
active-cache rule over served records, guarded the identity, and read and rendered the card.
KubeQuery could get one consistent reading only by riding the card's `Read` through an `Extra`
hook, so its binding lived in `clustercard` and every query read the card's whole facts: the
health, the sync rows, the kind catalog and every namespace name.

## Decision

**`services/cluster` resolves a cluster, in one place.** `Clusters().ReadActive(ctx, id, read)` reads the
record, finds its active cache with `CacheIsActive` over the runtime objects, runs `read` with
both as an `ActiveCluster`, then reads the identity again, and runs the whole reading once more
when it moved. A gone record is `ErrNotFound`, and an identity that moves under both attempts is
`ErrIdentityMoved`. `read` may run twice, so it sets what it returns and never appends to it.

Two readers use it. `clustercard` renders the card of the reading it is handed (`Read(ctx, svc,
ActiveCluster)`), and its freshness verdict reads the cache's health reading alone
(`Freshness(*ClusterCacheHealth)`). KubeQuery reads the chat's cluster itself
(`tools/kubequery/cluster.go`): inside one reading it reads the cache's health, and runs the
statement unless the verdict withholds the rows.

## Alternatives considered

- **Hand out an `ActiveCluster` and let the caller recheck.** Every reader would carry its own copy
  of the recheck and the retry, which is the duplication this removes. A reading handed out on its
  own can also be read after the identity moved.
- **Keep the resolution in `clustercard` and export it.** The active-cache rule is the cluster
  service's (`CacheIsActive`); a second copy over served records is how the two could come to
  disagree.
- **Skip a cache awaiting deletion, as the card's copy did.** A cache is marked only when its
  cluster is, and that cluster's chats go with it, so `CacheIsActive` needs no deletion check of its
  own, and `ReadActive` adds none.

## Consequences

- KubeQuery reads one health reading per query instead of the card's facts.
- `clustercard` imports no `tools`, and knows nothing of KubeQuery.
- `ReadActive` is on the `Clusters` interface, so every fake of it implements the method, including
  the resolver tests' fakes, which never call it.
- The freshness verdict reads its counts off the health reading. That is safe because a cluster
  with sync switched off reads `Paused` with no counts, and `Paused` answers before any count is
  read. A change to the health fold's counts changes the verdict.
