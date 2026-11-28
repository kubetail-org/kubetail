# Read contracts in GraphQL

How a field of the sidecar's schema is written for its [read contract](README.md), and what
the transport that carries it is called. Choose the contract there first: this guide says only
how GraphQL expresses it.

## Field names

A field puts its subject first and its contract last, as a mutation puts its verb last.

| Contract | Field | Result or frame type |
| --- | --- | --- |
| [Get](README.md#get) | `<noun>Get` | The value itself |
| [List](README.md#list) | `<noun>List` | A list of the element |
| [Fetch](README.md#fetch) | `<noun>Fetch` | `<Noun>FetchResult` |
| [State watch](README.md#state-watch) | `<noun>StateWatch` | The value itself |
| [Keyed state watch](README.md#keyed-state-watch) | `<noun>KeyedStateWatch` | The value, carrying its key |
| [Collection watch](README.md#collection-watch) | `<noun>CollectionWatch` | `<Noun>CollectionWatchFrame` |
| [Event feed](README.md#event-feed) | `<noun>EventFeed` | `EventFeedFrame` for a cluster `Event`; `<Noun>EventFeedFrame`, with a `<Noun>EventFeedFrameType` of its own, for any other element |
| [Sample feed](README.md#sample-feed) | `<noun>SampleFeed` | `<Noun>SampleFeedFrame` |
| [Cursor tail](README.md#cursor-tail) | `<noun>Tail` | `<Noun>TailFrame` |

A frame is an envelope around the data, so only the contracts that need one have a frame type.
The two notifications have no field.

The names each contract gives a subject. The table shows the pattern, not what exists: the
schema is the list of the fields there are.

| Subject | Query | Subscription |
| --- | --- | --- |
| Models | `modelsGet` | `modelsStateWatch` |
| Environment variables | `envVarsGet` | `envVarsStateWatch` |
| Cache health | `clusterCacheHealthGet` | `clusterCacheHealthKeyedStateWatch` |
| Clusters | `clusterList` | `clusterCollectionWatch` |
| Cluster events | `clusterEventList` | `clusterEventFeed` |
| Cluster data rate | `clusterDataRateSampleList` | `clusterDataRateSampleFeed` |
| Logs | `logsFetch` | `logsTail` |

- **A field always carries its noun.**
- **A scope is an argument.**
- **A field's description opens with its contract**: `"A Get of …"`,
  `"A collection watch of …"`.

## Absent values

A [Get](README.md#get) of an item or key that does not exist returns a null field or a
not-found error.

## Failures

A resolver over a `*Stream` uses `subscriptionStream`, which turns a non-nil `Err()` into one
terminal errors-only frame marked `extensions: {"subscriptionFailed": true}`.

## The transport's names

**`Query` and `Subscription` are the generic nouns, for the transport and nothing else**:
GraphQL's own `query` and `subscription` keywords, the host's `graphql_query`,
`graphql_subscribe`, `graphql_unsubscribe` and `log_window_subscribe` commands, and the
webview's `subscribe-exchange.ts` and `useFoldedSubscription`. Code that carries any read, and
knows no contract, takes these names. A field never does.
