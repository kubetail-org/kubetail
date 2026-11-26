# internal/services/cluster — the cluster subsystem

One file per family, and private leaves `internal/kubeconn`, `kubesync`, `kubestore`. **Leaves
speak native vocabulary, never records**; put mechanism in a leaf. → [ADR](../../../../docs/adr/2026-09-30-the-cluster-service-is-one-package-over-private-leaves.md).

**A cluster is a row in `app.db`; beehive holds its runtime.** The mirror (`mirror.go`) is the one
writer of each row's `Cluster` object, named by the row id, through `runtimeSpecOf(row)`.
`updated_at` moves on a user edit, never a reconcile. → [ADR](../../../../docs/adr/2026-09-16-clusters-are-rows-mirrored-into-beehive.md).

**The chain**: the `ClusterSource` pass imports one row per kube-context, never updated or deleted
→ the mirror → the cluster pass (a `kubeconn` claim, then the `ClusterCache` for the probed
identity) → the cache pass (discovery, `ClusterCachedKind` records) → each kind's sync.
**kubesync decides what exists; the records decide what is mirrored.** → [ADR](../../../../docs/adr/2026-08-09-beehive-control-plane.md), [ADR](../../../../docs/adr/2026-08-18-discovery-as-a-beehive-kind.md), [ADR](../../../../docs/adr/2026-08-28-arming-is-policy-never-interest.md).

## Controllers and passes

- A pass returns `Settled()`, `Unsettled()` or `Fail(err)`, never an error.
- Every controller registers `startupPass`. **A fold whose answer the store cannot see move owes a
  resync**, so `ClusterSource` (a file) and `Cluster` (a remote server) take a resync interval.
- Write status unconditionally: beehive suppresses equal bytes. No timestamps or counters on
  status. → [ADR](../../../../docs/adr/2026-09-02-cluster-conditions-two-subjects.md).
- A parent creates its children in the child's file, one write, no read. A deletion-pending pass
  writes nothing.
- **A cache carries `cacheFinalizer` from creation**, cleared only after `ForgetCache`,
  `kubestoreMgr.Remove` and the log file's removal succeed.
- A relayed value needs a `depends_on` edge; the owner edge is not one.
- **Both reconciles defer until the kubeconfig has been read**: keep the guards if you reorder
  startup.
- Tell a kubeconfig row by `kubeconfigContextOf(spec)`, never by name.
- **The collection watch diff compares frames with `==`**: no slice, map or pointer field on a
  record or `CollectionWatchFrame`. Timestamps are nullable `Time` bound to value `time.Time`.
- **Deletion is the mark.** `Clusters().Delete` marks and returns, refusing a row its source still
  declares (`ErrDeclaredBySource`); the toggles refuse a marked row as `ErrNotFound`. The mirror
  tears down and the row goes last. The connection gate reads the mark off the **row**
  (`connectableContext`).
- **One default cluster**, at most one `clusters.is_default` row by a partial unique index.
  `SetDefault` clears the old and sets the new in one transaction, and refuses
  (`ErrCannotBeDefault`) a disabled cluster or one its kubeconfig does not declare. Marking a row
  for deletion or disabling it clears its default; an archived row keeps it.
- **A failed `Connected` carries the probe's `kubeconn.Reason`**, `ProbeFailed` when unclassified;
  the webview names each. → [ADR](../../../../docs/adr/2026-11-03-connected-carries-the-probes-class-of-failure.md).
- **A failed kind keeps the reason `SyncFailed` and carries its class as `KindState.Failure`**
  (`kubeconn.ClassifyRequest`, empty when the failure is not a request's); the health fold passes
  the first offender's on as `failure`. → [ADR](../../../../docs/adr/2026-11-03-a-failed-sync-carries-its-class-of-failure.md).
- Neither cache controller writes a condition. → [ADR](../../../../docs/adr/2026-09-02-kind-records-mirror-the-catalog.md).
- Shared dependencies are fields of `deps`, never constructor parameters (`testutil_test.go`). A
  test fails one store call by swapping the client for a wrapper that embeds the real one
  (`failingClusterGets`, `scriptedWatches`); an app.db write by a `RAISE(ABORT)` trigger.
- Every participant is a named `lifecycle.Part`, never a stop closure. → [ADR](../../../../docs/adr/2026-08-16-lifecycle-composition.md).

## Identity

A context is not an identity. Ask `Lease.ConnFor(ctx, serverUID)`, which never waits; **never
correlate a connection with `State.ServerUID`**, which lags. → [ADR](../../../../docs/adr/2026-08-25-connection-carried-identity.md), [ADR](../../../../docs/adr/2026-08-27-identity-driven-retirement.md).

## Events

Three timelines keyed `(ObjectID, category)`, one read path (`events.go`). `Event.id` is unique
within a timeline only. → [ADR](../../../../docs/adr/2026-09-02-event-timelines.md).

## Streams and watches

- Every send goes through `sendFrame`; a bare send leaks the goroutine.
- One pump, `collectionWatch`, serves every collection watch and closes its snapshot with one
  `Bookmark` (nil item). Never add another.
- A watch that can die returns `*Stream[T]`; `Err` is set before `Frames` closes.
- The cluster list watch rereads on `KeyClusters` **and** the runtime `WatchList`.
- **The health state watch shares one read per cadence among its subscribers**
  (`sharedstatewatch.go`): the first starts the loop, the last stops it. A join asks for a read,
  so a first reading is one begun after the join. Each subscriber diffs against its own last
  frames. A failed read ends the run for all; the next subscriber starts a fresh one. → [ADR](../../../../docs/adr/2026-09-02-cache-health-fold.md).
- **Under `Connecting`, KubeQuery trusts a kind by its cookie**: `CachedData().ListedKinds` reads
  the catalog kinds with one, against the fold's `UnansweredKindRefs`, which is not on the wire. Discovery's `StoreFailed` and `IdentityMismatch`
  are the cache's verdict ahead of the per-kind fold. `Trusted()` and `ReadActiveCache` are
  cache-wide and still withhold a `Connecting` cache. → [ADR](../../../../docs/adr/2026-11-03-a-connecting-cache-answers-from-the-kinds-that-have-listed.md).
- The data-rate sample feed holds its own claim and subscribes to
  `Lease.ListenForDataRateChanges` before it sends `Lease.DataRates`; each nudge then sends the
  samples numbered above the last one sent. → [ADR](../../../../docs/adr/2026-11-02-a-series-is-a-third-kind-of-stream-and-the-pool-owns-its-clock.md).
- A cache at its size ceiling stops syncing, held by its record's condition; nothing evicts. → [ADR](../../../../docs/adr/2026-09-03-a-stopped-cache-is-held-by-its-record.md).

## Families and the GraphQL surface

Four families: `Clusters()`, `Caches()`, `CachedKinds()`, `CachedData()`. The scope is the entry
point, never an argument. Every GraphQL type binds 1:1 in `gqlgen.yml`; resolvers are one-liners.

- State watches are their own subscriptions, never a field on a record.
- `Cluster.caches` is a set; activeness is a live join (`CacheIsActive`).
- **`Clusters().ReadActive` is the one reading of a cluster at one identity**; `read` may run
  twice, so it must set, never append. **`CachedData().ReadActive`** reads the active cache by SQL,
  every statement on one snapshot. → [ADR](../../../../docs/adr/2026-09-27-the-cluster-service-resolves-a-cluster.md), [ADR](../../../../docs/adr/2026-10-13-a-cache-reading-is-one-snapshot.md), [ADR](../../../../docs/adr/2026-10-13-the-sidecars-own-mirror-reads-keep-their-connections.md).
- Every cluster mutation returns through `clusterErr` (`graph/util.go`).
- A resolver over a `*cluster.Stream` never uses `ptrSubscription`. → [ADR](../../../../docs/adr/2026-08-14-watch-failure-reporting.md).
- Every condition is built by `LiveCondition`; `Unconfirmed` is load-bearing on the wire. → [ADR](../../../../docs/adr/2026-08-09-liveness-conditions.md).

## The connection pool (`internal/kubeconn`)

A cluster is the only way to address a connection. → [ADR](../../../../docs/adr/2026-08-22-connections-addressed-by-cluster-id.md), [ADR](../../../../docs/adr/2026-08-23-one-connection-per-context.md).

- `Acquire` never fails or waits; `Conn` never dials.
- `RetryAndWait` returns once the probe it asked for ran. → [ADR](../../../../docs/adr/2026-08-30-retry-resolves-with-its-probe.md).
- **Every connection counts its wire bytes** with a dialer set as the `rest.Config`'s `Dial`, so
  every transport client-go builds counts, upgraded streams included. Nothing is persisted.
- **The pool samples every connection on one clock** (`datarate.go`): it reads each `conn`
  outside `mu` and appends under it. Samples are numbered for the entry's life; readers compare
  numbers, never times. A stored `At` keeps its monotonic reading. The caller closes
  `Lease.ListenForDataRateChanges`.
- **A debug build injects `KUBETAIL_FAULTS`** (`faults_debug.go`; a release build has stubs):
  `WithFaults` on `New` or `NewConnection` wraps the `rest.Config` inside the idle-read bound, and
  `store=fail` swaps the store manager kubesync arms with (`cluster.WithFaults`). A `quiet` watch
  keeps one sequence per collection path on the wrapper, which lives and dies with its
  connection; a `stalled` watch keeps nothing. →
  [`docs/development-switches.md`](../../../../docs/development-switches.md#injected-faults).
- Every non-stream request has an idle-read bound, never a deadline. → [ADR](../../../../docs/adr/2026-09-02-idle-read-bound.md).
- `WatchState` sends the claim's `State` on attach, then each pass. Every value is a level.
- Five `Observation[T]`s; a value survives a failure. `NotFound` and `Unsupported` are both a 404. → [ADR](../../../../docs/adr/2026-08-24-probe-engine.md).
- Reaching the server is `GET /api`. → [ADR](../../../../docs/adr/2026-08-25-connection-probe-dial.md).

## The supervisor (`internal/lib/supervisor`)

Kubernetes-free scheduling. A **job** runs and returns; a **worker** blocks until stopped (the
kind sync is the one worker). A `Result` is its schedule. Commit only on a change. `Remove` and
`Close` wait: never call them inside a `Run`. Gate a revival wake on `Suspended()`. → [ADR](../../../../docs/adr/2026-08-28-supervisor-vocabulary.md), [ADR](../../../../docs/adr/2026-08-28-jobs-and-workers.md).

## The sync engine (`internal/kubesync`)

Speaks cache ids, contexts, server UIDs and GVRs; never records.

- A clear's `fn` (`RunWithCacheSyncStopped`) must not call back into the Service.
- Two arming levels that AND: `TrackDiscovery` and `TrackKind`.
- Nothing syncs into a cache whose connection does not vouch for its `ServerUID`.
- A verdict is a state watch; `false` from `Get*State` means not yet observed.
- Snapshot `s.tracked` under `s.mu`, act outside it.
- The sweep writes only on a changed fingerprint. → [ADR](../../../../docs/adr/2026-09-02-discovery-sweep-rules.md).
- **The kind sync is one stream per run**: a quiet window reopens from the cookie, and an expired
  reopen relists in place. **Only a stream that has sent a bookmark reads `Stale`**, after
  `bookmarkStaleAfter` without a frame; a reopen that hangs reads `Resuming`. Keep
  `bookmarkStaleAfter` under `staleAfter`. → [ADR](../../../../docs/adr/2026-09-02-kind-sync-verdicts.md), [ADR](../../../../docs/adr/2026-09-27-a-quiet-watch-is-reopened-from-its-cookie.md), [ADR](../../../../docs/adr/2026-09-29-an-expired-quiet-reopen-relists-in-place.md), [ADR](../../../../docs/adr/2026-11-03-a-kind-reads-stale-only-when-its-bookmarks-stop.md).

## The store (`internal/kubestore`)

One SQLite file per cache behind a refcounted `Manager`. → [ADR](../../../../docs/adr/2026-08-26-cache-store-per-cache.md).

- Nothing on the read side creates a file.
- **A file whose close ran out of `sqlitepool.CloseBudget` is kept as stale**, and nothing deletes
  or creates its path until a `Clear`, `Remove` or open has waited its pools out: SQLite's close
  of a file's last connection deletes the WAL by path, which would be a fresh file's.
- `ForgetCache` before `Manager.Remove`; `ForgetKind` before `Store.ClearKind`.
- Every delete logs to `deletes` first; readers apply deletes before writes. → [ADR](../../../../docs/adr/2026-08-30-write-positions-and-the-deletes-log.md).
- Every statement is in `statements.go`, no `OnBoth`. → [ADR](../../../../docs/adr/2026-09-02-kubestore-sql-discipline.md).
- **Every SQLite file is owner-only** (umask 0o077, `sqlitepool.OpenWriter` chmods). → [ADR](../../../../docs/adr/2026-09-02-windows-cache-files-rely-on-the-profile-acl.md).
- One janitor per file. → [ADR](../../../../docs/adr/2026-09-02-kubestore-janitor.md).
- `Retention.SizeLimit` caps the three files summed. → [ADR](../../../../docs/adr/2026-09-03-bound-the-cache-by-total-size.md).
- **Side tables are written with their object, off the sanitized body.** A non-selectable selector
  writes no row: read `Requirements()`' flag, never the empty list. → [ADR](../../../../docs/adr/2026-09-27-kubequery-reads-tables-written-with-the-object.md).
- **KubeQuery reads `Store.Query` through `TEMP` views** (`query_views.sql`), the tool prompt's
  contract. Traps: `body()` caps inflation at 16 MiB; the `refs` view's lookup stays a `CROSS
  JOIN` (`TestTheRefsViewFindsATargetByItsIndex`); the `LIMIT` wrap is prepared first, then the
  statement alone; caller values bind, never paste. → [ADR](../../../../docs/adr/2026-09-27-kubequery-reads-views-on-a-connection-of-its-own.md).
- **The sidecar's own statements run through `Store.ReadOwn`**: one read-only transaction on the
  own-query pool, whose connections are kept. Each statement compiles once per file (`ownStmts`).
  Traps: never compile on the pool inside a reading; the row limit is
  written into the wrap, never bound. → [ADR](../../../../docs/adr/2026-10-25-the-sidecars-own-mirror-statements-compile-once-per-file.md).
- **A kind's count is `kind_counts`, trigger-maintained.** `KindsIn(namespace)` counts on every
  read through an index range per kind (`TestTheNamespacedKindCountsAreServedByIndexes`).
- The change signal is a coalesced ping, never a row delta. → [ADR](../../../../docs/adr/2026-08-26-store-change-ping-bus.md).
- **A cached-data watch** subscribes, snapshots, sends `Bookmark`, then reads once per burst past a
  `Cursor`. No read loads a body. → [ADR](../../../../docs/adr/2026-08-26-cached-data-read-loop.md), [ADR](../../../../docs/adr/2026-08-29-object-read-split.md).
