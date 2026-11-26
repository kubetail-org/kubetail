# Read contracts

A read is an API through which a consumer observes data a source owns: a **query**
returns once, and a **subscription** keeps delivering after the call returns. This covers Go
service methods, GraphQL fields, gRPC methods and the TypeScript that consumes them. An API that
starts something new (a probe, a mutation, a background task) is not a read.

Every read has a contract, its **read contract**, and is named for it. The contract follows
from what the data is and what the consumer does with it, never from the transport or the
serialized type.

The first half of this document is what a change needs: the taxonomy, how to choose a contract,
how to name it, and the steps. The second half is the reference: each
[query](#queries) and [subscription](#subscriptions) contract in full.

## Terms

| Term | Meaning |
| --- | --- |
| Family | What the consumer holds: a state, a collection, a history, a sequence, or nothing |
| Kind | What one element of the data is: a value, an item, an event, a sample, an entry, a notice |
| Contract | Exactly what an operation returns or delivers, and when. Each has a name: Get, collection watch |
| Value | A complete logical state |
| Item | A collection element with its own identity |
| Sample | One timestamped measurement. The timestamp says when it was taken and is neither its identity nor its order: a wall clock that steps can repeat or reverse timestamps |
| Entry | An immutable element of a sequence |
| Cursor | An opaque position in a sequence |
| Frame | An envelope around one subscription delivery |
| Snapshot | The state a subscription delivers first |

"Sample" is a noun here, never a verb: a contract over samples returns measurements already
taken, and measures nothing.

## The taxonomy

There are five families and eight kinds. Each kind has at most one query contract and one
subscription contract, and need not expose both.

| Family | The consumer holds | Kind | Query | Subscription |
| --- | --- | --- | --- | --- |
| **State** | The current value | Value | Get | State watch |
| | A map of current values by key | Keyed value | Get, by key | Keyed state watch |
| **Collection** | A map of items by id: the items, and which of them exist | Items | Get, List | Collection watch |
| **History** | A bounded window of past events | Events | List | Event feed |
| | A bounded window of past measurements | Samples | List | Sample feed |
| **Sequence** | Ordered, immutable entries and its position among them | Entries | Fetch | Cursor tail |
| **Notification** | Nothing: it is told that something changed | Change | None | Change notification |
| | Nothing: it is told that something happened | Occurrence | None | Broadcast notification |

## Choosing a contract

Choose by what the consumer holds, never by how the source stores the data. Take the first
family that fits:

1. The consumer is told that something happened, and not what the state now is:
   **Notification**. If the notice names something to re-read (a row, a key, a cache), it is a
   **change notification**. If nothing lies behind it to read (the machine woke), it is a
   **broadcast notification**.
2. The consumer reads an immutable sequence from a position: **Sequence**.
3. The consumer keeps a retained history: **History**. Events have identities; samples are
   timestamped measurements.
4. The consumer keeps items with their own identities, and which of them exist: **Collection**.
5. Otherwise it is **State**: one whole value, or a value per key when each changes on its own.

Where two contracts both seem to fit, one question decides:

| Between | The question |
| --- | --- |
| Value and keyed value | Do the keys change independently, often enough that resending all of them is wasteful? Then it is a keyed value. |
| Keyed value and collection | Must the subscription itself say which keys exist? Then it is a collection. |
| Collection and events | Does the source announce removals? Then it is a collection. |
| Events and samples | Is an element something that happened, or a measurement? |
| History and sequence | Does the source replay a retained window, or serve entries from a position the consumer names? |
| Change and broadcast | Is re-reading a source enough to react correctly? Then it is a change notification. |
| List and Fetch | Is the continuation token a position in the sequence, which a tail can start from? Then it is a Fetch. |

The contract, not the serialized type, sets the name. When a list goes from `[a, b, c]` to
`[a, b, c, d]`, a Get, a List and a state watch each deliver `[a, b, c, d]`, a collection watch
delivers `Added(d)`, and a change notification delivers a notice that carries nothing.

The same data is often layered. `appdb` publishes a change notification per table; the chat
service listens, re-reads and diffs, and publishes a collection watch. The layers have different
contracts and different names.

### Pairing a query with a subscription

A query and a subscription over the same data share their subject, their element and their
types:

- A Get returns the value its state watch delivers.
- A List returns the items its collection watch delivers, the events its event feed delivers, or
  the samples its sample feed delivers.
- A Fetch returns the entries its cursor tail delivers, and their cursors are compatible.
- A notification has no query of its own. A change notification prompts a query of the source.

A watch or a feed delivers its whole state first, so a query beside one is for a caller that
reads once and leaves. Never add a query only to initialize a subscription.

## Naming

Each family's subscriptions share a verb: **Watch** for State and Collection, **Subscribe** for
History, **Tail** for Sequence and **Listen** for Notification. Go puts the verb first. GraphQL
puts the subject first, as its mutations do. gRPC methods are named as Go's are, and a
subscription there is a server stream.

| Contract | Go method | GraphQL field | Result or frame type |
| --- | --- | --- | --- |
| Get | `Get<Noun>` | `<noun>Get` | The value itself |
| List | `List<Noun>s` | `<noun>List` | A list of the element |
| Fetch | `Fetch<Noun>` | `<noun>Fetch` | `<Noun>FetchResult` |
| State watch | `Watch<Noun>State` | `<noun>StateWatch` | The value itself |
| Keyed state watch | `Watch<Noun>KeyedState` | `<noun>KeyedStateWatch` | The value, carrying its key |
| Collection watch | `Watch<Noun>Collection` | `<noun>CollectionWatch` | `<Noun>CollectionWatchFrame` |
| Event feed | `Subscribe<Noun>EventFeed` | `<noun>EventFeed` | `EventFeedFrame` |
| Sample feed | `Subscribe<Noun>SampleFeed` | `<noun>SampleFeed` | `<Noun>SampleFeedFrame` |
| Cursor tail | `Tail<Noun>FromCursor` | `<noun>Tail` | `<Noun>TailFrame` |
| Change notification | `ListenFor<Noun>Changes` | Go only | The key alone |
| Broadcast notification | `ListenFor<Noun>Notifications` | Go only | `<Noun>Notification` |

A frame is an envelope around the data, so only the contracts that need one have a frame type.

The names each contract gives a subject. The table shows the pattern, not what exists: the
schema and the services are the list of the operations there are.

| Subject | Go query | Go subscription | GraphQL query | GraphQL subscription |
| --- | --- | --- | --- | --- |
| Models | `GetModels` | `WatchModelsState` | `modelsGet` | `modelsStateWatch` |
| Environment variables | `GetEnvVars` | `WatchEnvVarsState` | `envVarsGet` | `envVarsStateWatch` |
| Cache health | `GetCacheHealth` | `WatchCacheHealthKeyedState` | `clusterCacheHealthGet` | `clusterCacheHealthKeyedStateWatch` |
| Clusters | `ListClusters` | `WatchClusterCollection` | `clusterList` | `clusterCollectionWatch` |
| Cluster events | `ListClusterEvents` | `SubscribeClusterEventFeed` | `clusterEventList` | `clusterEventFeed` |
| Cluster data rate | `ListClusterDataRateSamples` | `SubscribeClusterDataRateSampleFeed` | `clusterDataRateSampleList` | `clusterDataRateSampleFeed` |
| Logs | `FetchLogs` | `TailLogsFromCursor` | `logsFetch` | `logsTail` |

Rules that apply to every contract:

- **The noun is in its natural number.** A collection's noun is one item, so it is singular in
  GraphQL: `clusterCollectionWatch`. A state's noun is the value, so it is plural when the value
  is a list or a set: `modelsStateWatch`.
- **A Go method drops the noun when it is its receiver's subject**: `Clusters().List()`,
  `Clusters().WatchCollection()`. A GraphQL field always carries it.
- **A scope is a `By<Scope>` suffix in Go and an argument in GraphQL.**
- **A qualifier is never repeated.** A noun that already ends in `State` takes no second one:
  `authStateWatch`. A noun of Kubetail's own ending in `Status` is renamed to end in `State`.
- **A service or store method that opens with `Get`, `List`, `Fetch`, `Watch`, `Tail` or
  `Listen` has that verb's contract.** So does a feed method that opens with `Subscribe`;
  generic pub/sub code may use `Subscribe` freely. A method that starts something new to produce
  its answer takes another verb (`Probe`, `Resolve`), as does a poller or a background task;
  reading, filtering and shaping stored data starts nothing. An accessor on a value in memory is
  not a read and is named as Go names it.
- **`Query` and `Subscription` are the generic nouns, for the transport and nothing else**:
  GraphQL's own `query` and `subscription` keywords, the host's `graphql_query`,
  `graphql_subscribe`, `graphql_unsubscribe` and `log_window_subscribe` commands, and the
  webview's `subscribe-exchange.ts` and `useFoldedSubscription`.
- **A name from outside keeps its meaning**: a Kubernetes watch or list, the kubelet's `follow`
  parameter, the browser's `fetch`, `net.Listen` and a library's `Subscribe`.
- **A GraphQL field's description opens with its contract**: `"A Get of …"`,
  `"A collection watch of …"`.

In the webview, an operation is named for its field, Pascal-cased (`ClusterCollectionWatch`),
its document constant adds `Document`, and a hook that serves the operation alone is `use` plus
the operation name (`useClusterCollectionWatch`). A provider's hook that joins, sorts or shapes
what it holds is named for what it returns (`useClusters`, `useChats`).

## Kubetail's policies

These are this project's rules, not properties of the contracts.

- **Nothing polls a query.** A value that changes without the caller acting gets a
  subscription. A query alone is for a value only the caller's own writes change, and for a
  Fetch.
- **A query with no caller is removed.**
- **A mutation that writes a Get's value returns that value**, so the caller needs no second
  read: `envVarSet` returns what `envVarsGet` does.
- **Notifications stay inside the sidecar.** Nothing on the wire tells a client to re-read.
- **A collection never embeds another.** The consumer joins collections by id.
- **A collection watch never filters.** An item awaiting deletion arrives as an ordinary
  `Modified` carrying its mark. `Deleted` means it is gone, and only the `Deleted` is promised.

## In the webview

- **Each subscription contract has one reducer, named for it**: `stateWatchReducer`,
  `keyedStateWatchReducer`, `collectionWatchReducer`, `eventFeedReducer`, `sampleFeedReducer`
  and `cursorTailReducer`. Fold a subscription with its contract's reducer and no other.
- **Detect a frame by its `type`, never by which field is missing.** A nested non-null field
  that errors nulls its parent, so a null item also arrives on ordinary changes: a `Bookmark` is
  `type === 'Bookmark'`. A change that carries no item is dropped, not folded.
- **One stream is shared among the consumers of an operation**, so a consumer that mounts late
  joins a stream already past its first delivery. Such a consumer subscribes unshared
  (`useFoldedSubscription`'s `unshared`) or reads from a provider that holds the one
  subscription. A sample feed is always subscribed unshared.

## Adding an operation

1. Choose the contract with [Choosing a contract](#choosing-a-contract), and check
   [Pairing a query with a subscription](#pairing-a-query-with-a-subscription) that a query is
   needed at all.
2. Name the method, the field and the result or frame type from [Naming](#naming). A query
   beside a subscription takes its subject and its types.
3. Implement the contract in full, from its section below. A subscription that sends a snapshot
   with no `Bookmark`, or a state watch that sends nothing until the first change, is a
   different contract and misleads every consumer written against the name.
4. In the webview, fold a subscription with the reducer for its contract.
5. Test a query's result and its empty result. Test a subscription's first delivery, one later
   delivery and its ending, each on its own.

## Changing an operation

- **The name is the contract.** Read an operation's contract from its name, never from its
  return type, its transport or its storage, and keep the contract when changing the
  implementation.
- **Code that breaks its named contract is a bug, not a precedent.** Fix it or report it; never
  copy it into a new operation or write a consumer that depends on it.
- **A change in what the consumer must do is a change of contract.** Choose the new contract,
  and change the name and every consumer in the same change.

## Adding a contract

Reuse a contract whenever its semantics match. A new domain object is not a new kind.

Add a contract only when the consumer must handle the result, the first delivery, the later
deliveries, a drop or the ending differently. A new contract takes a verb the taxonomy does not
use, such as `Search` to find matching items, `BatchGet` to get several the caller names, or
`Aggregate` to compute a summary. A watch that delivers patches would be a new contract too.

## Queries

| Contract | Returns | Nothing to return | The caller's job |
| --- | --- | --- | --- |
| **Get** | One whole value, or one item by id or key | The transport's not-found result | Replace |
| **List** | The elements of a collection or a retained history, or a page of them | An empty list | Enumerate |
| **Fetch** | A bounded batch of a sequence's entries, with continuation cursors | An empty batch, with its cursors | Append or prepend |

### Get

A Get returns one whole value: a scalar, an object, a map or a list. A value that is a list is
still a Get when the caller treats the list as one value and not as items it manages one by one.

Three answers are distinct, and none of them is an error:

- **Absent:** the item or key does not exist. That is the transport's not-found result: a null
  field or a not-found error in GraphQL, `(value, false)` or a not-found error in Go,
  `NOT_FOUND` in gRPC.
- **Unknown:** the value exists but is not yet available. The value says so itself
  (`Restoring`). A singleton state is never absent, only unknown.
- **Empty:** the value is known and holds nothing, as a list of no models is.

### List

A List returns the items, events or samples that match its arguments, all of them or one page.
The kind sets identity and order, not the List:

| Kind | Identity | Order |
| --- | --- | --- |
| Items | The item's id | Stated where it matters |
| Events | The event's id | Newest first |
| Samples | The position in its series | The order they were taken in, oldest first |

### Fetch

A Fetch returns a bounded batch of entries from a position in a sequence, in the sequence's
order, with opaque cursors for continuing in either direction. A filter does not make a Fetch
something else.

Paging is not what separates a List from a Fetch. A List's page token is good only for the next
page of that List. A Fetch's cursor is a position in the sequence itself, and a tail can start
from it.

## Subscriptions

| Contract | First delivery | Later deliveries | May lose | The subscriber's job | After a drop | Ends |
| --- | --- | --- | --- | --- | --- | --- |
| State watch | The whole value | The whole value again on each change | Intermediate values | Replace | Start again | Never |
| Keyed state watch | One whole value per key | The whole value of the key that changed | Intermediate values of a key | Replace by key | Start again | Never |
| Collection watch | Every item, then one `Bookmark` | One `Added`, `Modified` or `Deleted` per item | Intermediate states of an item, and an item that came and went between two reads | Reconcile by id | Start again | Never |
| Event feed | The retained events, then one `Bookmark` | New events; a repeat upserts by id | Intermediate states of an event | Upsert by id and cap | Start again | Never |
| Sample feed | The whole retained window, even if empty | Only the samples taken since the last | Samples that left the window before they were sent | Append and trim | Start again | Never |
| Cursor tail | The entries past the start position | Each entry, in order | Nothing | Append and keep the cursor | Resume from the cursor | With a final `Ended` frame |
| Change notification | Nothing | A key-only notice | Every notice for a key but one | Re-read the source | Nothing is replayed | When the publisher closes |
| Broadcast notification | Nothing | Each occurrence, in order, never merged | The oldest occurrences, once the subscriber falls behind the buffer | React to each | Nothing is replayed | When the publisher closes |

"Never" means the contract has no ending of its own. Every subscription still stops when its
context is cancelled, and a broken source ends one with an error (see
[Endings and failures](#endings-and-failures)).

Two rules hold for every contract that has a first delivery:

- **The first delivery and the later ones are one observation of the source.** No change falls
  between them. A publisher listens to its source before it reads it, so a change that lands
  between the two costs a repeated delivery, never a lost one. A publisher whose source sends no
  change signal reads it again on a cadence instead.
- **A layer between the publisher and the subscriber keeps the contract.** Anything that shares,
  buffers, coalesces, deduplicates or retries a subscription may lose only what the contract's
  "May lose" allows, gives every new subscriber the contract's first delivery, and never
  restarts a tail that sent `Ended`.

The sections below add what the table cannot say.

### State watch

The subscriber holds a copy of the current value and nothing else. What the watch delivers is
enough to rebuild it from nothing, so it never needs a second call.

- **There is no `Bookmark`.** The first frame is sent at once, and its arrival is the boundary
  between "not known yet" and "known". No frame yet is loading.
- **A value not yet known says so in the value** (`Restoring`), rather than delaying the first
  frame.
- **Every frame is a level, not an edge.** The subscriber replaces what it holds and merges or
  diffs nothing. A frame equal to the last one is harmless. Some frames may have been skipped,
  so never infer what changed from two in a row.

### Keyed state watch

A state watch over many keys on one subscription. Each frame carries its key, and the
subscriber keeps a map by key, replacing the entry on each frame.

- **The first delivery is one frame per key that has a value now.**
- **There is no `Bookmark` and no removal frame.** The subscriber cannot tell "no keys" from
  "not yet", or when a key is gone.
- **Membership comes from somewhere else.** Join the map onto the collection watch that owns the
  keys, and ignore entries with no owner. A keyed state watch is never the source of a set of
  keys.

### Collection watch

The publisher mirrors a keyed set, one item per frame. The subscriber keeps a map by `id`:
upsert on `Added` and `Modified`, remove on `Deleted`.

- **The snapshot is one `Added` per item, in no promised order, then exactly one `Bookmark`.**
  The `Bookmark` carries no item. An empty set sends the `Bookmark` alone.
- **Everything before the `Bookmark` is loading.** Never draw an empty state before it. Fold the
  `Bookmark` away; never key it.
- **A `Deleted` carries the item's last-known state.**
- **A frame of a scoped watch carries its scope** (`cacheID`, and for objects `apiVersion` and
  `resource`). Reject a frame whose scope is not the one now subscribed to: a superseded
  subscription can still deliver.

### Event feed

The publisher retains a bounded history and replays it on subscribe. The subscriber upserts by
the event's id.

- **The snapshot is the retained events, newest first, then exactly one `Bookmark`.** Everything
  before it is loading.
- **An event that changes is sent again under the same id**, as a run that gains an occurrence
  is.
- **Order by the event's own fields, not by arrival.**
- **The publisher prunes old events and never announces it.** There is no `Deleted`, so the
  subscriber caps what it keeps: a long-lived subscriber's history can outgrow the publisher's.

### Sample feed

- **The first frame is the whole retained window, oldest first, sent even if empty.** Its
  arrival is the boundary a `Bookmark` marks elsewhere.
- **Each later frame holds only the samples taken since the last one sent**, in the order they
  were taken.
- **A sample is sent on every tick, zero included.** A quiet source is a flat line, not silence.
- **Every frame carries `intervalSeconds` and `windowSeconds`.** The subscriber appends each
  frame's samples and keeps the last `windowSeconds ÷ intervalSeconds`.
- **Count samples; never trim or order by time.**

### Cursor tail

The source is an append-only sequence. The subscriber consumes it from a position and holds no
state the publisher gave it: an entry, once delivered, is the subscriber's.

- **Delivery starts at the position the subscriber names**: a cursor, a time, or "now" when it
  names neither. Nothing is replayed from before it.
- **Each entry arrives in the source's order and carries the cursor that names it.** A source
  made of several streams orders each stream; across streams the order is arrival.
- **A cursor is opaque.** Never parse or compare one.
- **The subscriber appends.** It never upserts, reorders or deduplicates by content.
- **A `Notice` frame may arrive between entries**, with something the reader should know.
- **To resume after a drop, tail again from the same start, naming the cursor of the last entry
  consumed.** A source made of several streams has one such cursor per stream. Each stream goes
  on past the entry named, so an entry received but not yet remembered arrives again.
- **The last frame is `Ended`, saying why, and it is final.** Show its reason. Tailing again
  from the same position would end the same way. A drop without `Ended` is a failure to resume
  from.

### Change notification

The subscriber is told that something changed and nothing about what the state now is. A Go API
inside the sidecar.

- **A notice carries the key that changed and no value.** A value handed through a queue that
  coalesces would be written from a state that has already moved on.
- **Notices for one key coalesce.** A subscriber that falls behind gets one notice, not one per
  change, so never count them. A notice for one key is never lost behind another key's.
- **Listen first, then read.** A change that lands between a read and a later listen is missed.
- **On each notice, re-read the source for that key.** The read is the truth; the notice is only
  the reason to make it.
- **The receiver closes when the publisher does.** Close it when done.

### Broadcast notification

The subscriber is told that something happened, with nothing behind it to re-read. A Go API
inside the sidecar.

- **One delivery per occurrence, in order, each with its own payload**: what happened and when.
  Occurrences are never merged.
- **The publisher never blocks.** Its buffer is bounded, so a subscriber that falls further
  behind than the buffer loses the oldest occurrences, and nothing says so. Keep up: do slow
  work elsewhere. If missing an occurrence would be wrong, the contract is the wrong one.
- **The channel closes when the publisher does, or when the subscriber cancels.** Cancel when
  done; an abandoned channel leaks its feeder.

### Endings and failures

- **Go.** A Watch, Feed or Tail whose source can fail returns a `*Stream[T]`: `Frames`, plus
  `Err()`. `Frames` closes on every exit, and `Err()` is read after it closes. A nil `Err()` is a
  clean end. A subscribe that fails before the first frame returns the error from the method
  instead.
- **GraphQL.** A resolver over a `*Stream` uses `subscriptionStream`, which turns a non-nil
  `Err()` into one terminal errors-only frame marked `extensions: {"subscriptionFailed": true}`.
  The webview's subscribe exchange treats that frame, and any completion, as a drop: it reports,
  marks the subscription disconnected and reconnects.
- **A reconnect is a new subscription.** The first delivery is sent again, and the subscriber
  starts from nothing: an empty map, an empty history. A cursor tail is the exception: it
  resumes from its cursor.
- **The only ending that is part of a contract is a tail's `Ended`.** The exchange would
  reconnect on the completion that follows it, so the tail's subscriber unsubscribes on `Ended`
  and the tail is not opened again.

→ [ADR: name every read for its family, kind and operation](../adr/2026-11-25-name-every-data-access-for-its-family-kind-and-operation.md),
[ADR: call a query or a subscription a read](../adr/2026-11-26-call-a-query-or-a-subscription-a-read-and-its-contract-a-read-contract.md),
[ADR: delta-watch protocol](../adr/2026-08-09-delta-watch-protocol.md),
[ADR: watch failure reporting](../adr/2026-08-14-watch-failure-reporting.md),
[ADR: a series is a third kind of stream](../adr/2026-11-02-a-series-is-a-third-kind-of-stream-and-the-pool-owns-its-clock.md).
