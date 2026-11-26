# Read contracts in the webview

How the webview names and consumes a read, for its [read contract](README.md). The contract
is the field's: take it from the field's name, and take the name from [GraphQL](graphql.md).

## Names

- **An operation is named for its field, Pascal-cased.**
- **Its document constant adds `Document`.**
- **A hook that serves the operation alone is `use` plus the operation name**:
  `useClusterDataRateSampleFeed`.
- **A provider's hook that joins, sorts or shapes what it holds is named for what it returns**:
  `useClusters`, `useChats`.

The names each field gives. The table shows the pattern, not what exists.

| Subject | Field | Operation | Document |
| --- | --- | --- | --- |
| Models | `modelsStateWatch` | `ModelsStateWatch` | `ModelsStateWatchDocument` |
| Environment variables | `envVarsGet` | `EnvVarsGet` | `EnvVarsGetDocument` |
| Cache health | `clusterCacheHealthKeyedStateWatch` | `ClusterCacheHealthKeyedStateWatch` | `ClusterCacheHealthKeyedStateWatchDocument` |
| Clusters | `clusterCollectionWatch` | `ClusterCollectionWatch` | `ClusterCollectionWatchDocument` |
| Cluster data rate | `clusterDataRateSampleFeed` | `ClusterDataRateSampleFeed` | `ClusterDataRateSampleFeedDocument` |
| Logs | `logsFetch`, `logsTail` | `LogsFetch`, `LogsTail` | `LogsFetchDocument`, `LogsTailDocument` |

## Folding a subscription

**Each subscription contract has one reducer, named for it**, in `src/lib/graphql/`. Fold a
subscription with its contract's reducer and no other. The reducer does the subscriber's job
its contract states.

| Contract | Reducer |
| --- | --- |
| [State watch](README.md#state-watch) | `stateWatchReducer` |
| [Keyed state watch](README.md#keyed-state-watch) | `keyedStateWatchReducer` |
| [Collection watch](README.md#collection-watch) | `collectionWatchReducer` |
| [Event feed](README.md#event-feed) | `eventFeedReducer` |
| [Sample feed](README.md#sample-feed) | `sampleFeedReducer` |

A [cursor tail](README.md#cursor-tail) holds no state its publisher gave it, so it has no
reducer: its client appends each entry and keeps the cursor.

A scoped collection watch folds through `useCacheCollectionWatch`, which rejects a frame whose
scope is not the one now subscribed to.

## Reading a frame

**Detect a frame by its `type`, never by which field is missing.** A nested non-null field that
errors nulls its parent, so a null item also arrives on ordinary changes: a `Bookmark` is
`type === 'Bookmark'`. A change that carries no item is dropped, not folded.

## Shared and unshared streams

**One stream is shared among the consumers of an operation**, so a consumer that mounts late
joins a stream already past its first delivery. Such a consumer subscribes unshared
(`useFoldedSubscription`'s `unshared`) or reads from a provider that holds the one
subscription. A sample feed is always subscribed unshared.

## Drops and endings

- **The subscribe exchange treats a `subscriptionFailed` frame, and any completion, as a
  drop**: it reports, marks the subscription disconnected and reconnects.
- **A tail's subscriber unsubscribes on `Ended`.** The exchange would reconnect on the
  completion that follows it.
