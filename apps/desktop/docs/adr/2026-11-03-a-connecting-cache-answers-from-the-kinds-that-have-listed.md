---
title: A Connecting cache answers KubeQuery from the kinds that have listed
date: 2026-11-03
scope: sidecar
status: Accepted
---

# A Connecting cache answers KubeQuery from the kinds that have listed

## Context

KubeQuery withheld every row of a cache that was not `Trusted()`, and `Connecting` was not
trusted. A cache reads `Connecting` while any kind has reported no state this session
(`readCacheHealth`). Kind state lives in memory, so every app start began with a cache that
withheld rows its file already held, until each kind's sync had run once.

The rule protects one thing: a kind never listed has no rows, and a query over it answers "none",
which the model reads as a fact. A kind listed in an earlier session does not have that problem.
Its rows are old, not missing.

The store already records which kinds have listed. A kind's cookie (`cookie/<apiVersion>/<resource>`
in `cluster_meta`) is written by its list's commit and cleared by a relist's first page. The
`refs` view's `to_listed` reads the same fact.

## Decision

**Under `Connecting`, KubeQuery decides trust per kind.** `CachedData().ListedKinds` reads the
catalog kinds that have a cookie, through `kubestore.ListedKinds`: one join on the same key the
`refs` view builds. The health fold names the kinds with no state yet
(`ClusterCacheHealth.UnansweredKindRefs`, not served on the wire).
`clustercard.ConnectingFreshness` splits every kind not `Watching` by its cookie:

- A kind with a cookie answers, and its rows may be out of date. A kind whose reason is
  `RefusedCacheWide` (`IdentityMismatch`, `StoreFailed`), the predicate `Trusted()` refuses
  through, is named under `unknown` instead, so the per-kind path never serves what the
  cache-wide one withholds. With no kind `Watching`, the
  answer's freshness is `last-known` with reason `Connecting`. With some kind `Watching`, it is
  `partial`, and `notWatching` names the kinds behind.
- A kind with no cookie is named: under `syncing` while it has said nothing or its list is
  running, under `unknown` when no list is running (`SyncFailed`, `NoConnection`), so the model
  does not promise rows that are not coming. A relist in place keeps the kind's old rows, so
  either's rows are incomplete or out of date rather than absent; the prompt tells the model an
  object of such a kind missing from a result may exist.
- Until some kind has rows, one `Watching` or one with a cookie, nothing answers, and the
  freshness is `syncing` as before.

`UnansweredKindRefs` is not on the wire, and the health stream does not compare it: a change to
it alone sends no frame.

**The cache-wide refusals stand.** `Trusted()` is unchanged: the empty verdict, `Connecting`,
`SizeLimit`, `StoreFailed` and `IdentityMismatch` are not trusted, and KubeQuery withholds every
row under the other four. The health fold reads discovery's `IdentityMismatch` as the cache's
verdict, as it does `StoreFailed`: a context reaching another server must not read `Connecting`
while its kinds have yet to report the mismatch for themselves, since `Connecting` now answers.

`ReadActiveCache`, the log tools' and the log service's reading, still requires `Trusted()`. The
log sweep (`podsGone`) treats a pod missing from the mirror as gone for good, which old rows
cannot promise. The chat's card is unchanged: it does not read the store's cookies.

## Alternatives considered

- **Answer a Connecting cache only when every kind behind has listed.** One kind that never
  listed, such as a CRD installed since, would withhold every other kind's rows.
- **Hide the rows of a kind that has not listed, in the views.** The model would see no rows,
  which reads as "none": the false answer the rule exists to prevent. Naming the kind is the
  signal.
- **Fold the cookies into the health gauge.** The gauge is read on a cadence for every cache. It
  would open every cache's file each tick, for a fact only KubeQuery reads.

## Consequences

After a restart, KubeQuery answers from the file as soon as the cache is armed, and says the rows
may be out of date. The answer can now carry a `syncing` list, which the KubeQuery prompt defines.

Health, the cookies and the rows are three reads, not one snapshot. A kind that commits between
them is named as syncing though listed, which is harmless. A relist in place keeps the kind's old
rows until its commit, so a kind whose cookie clears between the reads still holds a full set.

## Revisit when

The log readers need old rows while the cache reconnects, or kind state survives a restart.
