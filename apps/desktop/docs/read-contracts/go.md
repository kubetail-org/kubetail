# Read contracts in Go and gRPC

How a Go method in the sidecar, and a gRPC method, is written for its
[read contract](README.md). Choose the contract there first: this guide says only how Go
expresses it.

## Method names

A method puts its contract's verb first.

| Contract | Method |
| --- | --- |
| [Get](README.md#get) | `Get<Noun>` |
| [List](README.md#list) | `List<Noun>s` |
| [Fetch](README.md#fetch) | `Fetch<Noun>` |
| [State watch](README.md#state-watch) | `Watch<Noun>State` |
| [Keyed state watch](README.md#keyed-state-watch) | `Watch<Noun>KeyedState` |
| [Collection watch](README.md#collection-watch) | `Watch<Noun>Collection` |
| [Event feed](README.md#event-feed) | `Subscribe<Noun>EventFeed` |
| [Sample feed](README.md#sample-feed) | `Subscribe<Noun>SampleFeed` |
| [Cursor tail](README.md#cursor-tail) | `Tail<Noun>FromCursor` |
| [Change notification](README.md#change-notification) | `ListenFor<Noun>Changes` |
| [Broadcast notification](README.md#broadcast-notification) | `ListenFor<Noun>Notifications` |

The two notifications are written in Go alone. A change notification delivers the key and
nothing else; a broadcast notification delivers a `<Noun>Notification`.

The names each contract gives a subject. The table shows the pattern, not what exists: the
services are the list of the methods there are.

| Subject | Query | Subscription |
| --- | --- | --- |
| Models | `GetModels` | `WatchModelsState` |
| Environment variables | `GetEnvVars` | `WatchEnvVarsState` |
| Cache health | `GetCacheHealth` | `WatchCacheHealthKeyedState` |
| Clusters | `ListClusters` | `WatchClusterCollection` |
| Cluster events | `ListClusterEvents` | `SubscribeClusterEventFeed` |
| Cluster data rate | `ListClusterDataRateSamples` | `SubscribeClusterDataRateSampleFeed` |
| Logs | `FetchLogs` | `TailLogsFromCursor` |

- **A method drops the noun when it is its receiver's subject**: `Clusters().List()`,
  `Clusters().WatchCollection()`.
- **A scope is a `By<Scope>` suffix**: `Caches().WatchCollectionByCluster`.
- **A service or store method that opens with `Get`, `List`, `Fetch`, `Watch`, `Tail` or
  `Listen` has that verb's contract.** So does a feed method that opens with `Subscribe`;
  generic pub/sub code may use `Subscribe` freely.
- **A method that starts something new to produce its answer takes another verb** (`Probe`,
  `Resolve`), as does a poller or a background task. Reading, filtering and shaping stored data
  starts nothing.
- **An accessor on a value in memory is not a read** and is named as Go names it.

## Absent values

A [Get](README.md#get) of an item or key that does not exist returns `(value, false)` or a
not-found error.

## Endings and failures

- **A Watch, Feed or Tail whose source can fail returns a `*Stream[T]`**: `Frames`, plus
  `Err()`. `Frames` closes on every exit, and `Err()` is read after it closes. A nil `Err()` is
  a clean end.
- **A subscribe that fails before the first frame returns the error from the method.**
- **Every subscription ends when its `ctx` does.**

## gRPC

A gRPC method is named as a Go method is. A subscription is a server stream. A Get of an item
or key that does not exist returns `NOT_FOUND`.
