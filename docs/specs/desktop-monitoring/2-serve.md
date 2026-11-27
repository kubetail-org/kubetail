# Step 2 — Serve

Prerequisite: step 1 on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The webview can read incidents and their occurrences. No screen draws them yet.

## Design

Each read takes its contract from [`apps/desktop/docs/read-contracts/`](../../../apps/desktop/docs/read-contracts/README.md).

| Read | Contract | Go | GraphQL | Frame |
| --- | --- | --- | --- | --- |
| Incidents | Collection watch | `WatchIncidentCollection` | `incidentCollectionWatch` | `IncidentCollectionWatchFrame` |
| An incident's occurrences | Event feed | `SubscribeIncidentOccurrenceEventFeed` | `incidentOccurrenceEventFeed(incidentID: IncidentID!)` | `IncidentOccurrenceEventFeedFrame` |

Neither has a query beside it: each delivers its whole state first, and no caller reads once and leaves.

### Incidents

- **`incidentCollectionWatch` carries every cluster's incidents** and takes no scope. The webview joins them onto clusters by `clusterID`, as it joins caches. One watch serves both the window's list and the bell.
- **The service listens on `KeyIncidents` and `KeyClusters` before its first read**, then re-reads and diffs, as the memory stream does. A cluster's delete cascades to its incidents and notifies `KeyClusters` alone. An incident deleted with its cluster, or by retention, arrives as `Deleted` with its last-known state.
- **`Incident`** carries its id, `clusterID`, the subject's group, kind, namespace and name, `signal` (an enum of the five), `firstSeenAt`, `open`, `occurrenceCount` (every occurrence it has had, the trimmed ones included), `lastStartedAt` (its newest occurrence's start) and `lastSeenAt` (null while it is open). The count and the times ride on the record, so the list draws without a second read, and a new or ended occurrence arrives as `Modified`. An open incident's last-seen time moves with every reading and is not served, so a signal that holds sends no frame. Each field is a scalar or a nullable `Time`, so the diff's `==` holds.
- **`IncidentID`** is a scalar of its own, as `ChatID` and `MemoryID` are.

### Occurrences

An incident's occurrences are a retained history: the newest 100, the oldest trimmed unannounced. That makes them an event feed.

- **The snapshot is the retained occurrences, newest first, then one `Bookmark`.** Each later frame is an occurrence that started or ended, sent under its id; an ended one is sent again with its `endedAt`.
- **`IncidentOccurrenceEventFeedFrame`** is `type: EventFeedFrameType!` and `occurrence: IncidentOccurrence`, null on `Bookmark`. `IncidentOccurrence` carries its `id` (`IncidentOccurrenceID`, a scalar of its own, as `MessageID` is), its `incidentID`, `startedAt` and a nullable `endedAt`.
- **The frame carries its scope**: the webview drops a frame whose `incidentID` is not the one now subscribed to.
- **An id that names no incident answers the `Bookmark` alone**, and so does an incident deleted under the feed: its `Deleted` on `incidentCollectionWatch` is what removes it.
- The service listens on `KeyIncidents` before it reads, and on each notice re-reads the incident's occurrences and sends those that are new or changed.

### Both

- **Each Go method returns the monitor package's own `*Stream[T]`**, as `memory` and `chat` each have one, and each resolver goes through `subscriptionStream`, so a failed read ends the subscription with a `subscriptionFailed` frame.
- **Each field's description opens with its contract**: "A collection watch of every cluster's incidents …", "An event feed of one incident's occurrences …".

The per-cluster switch is already served: `Cluster.monitoringEnabled` and `clusterMonitoringEnabledSet`.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Service reads | Planned | — |
| 2. Schema and resolvers | Planned | — |

1. **Service reads.** The two methods on the monitor service, each listening before it reads.
2. **Schema and resolvers.** A `# --- Incidents ---` section in `schema.graphqls`, `make generate-desktop`, the resolvers over `subscriptionStream`, and resolver tests through `graph.NewServer`.

## Acceptance

- A subscriber to `incidentCollectionWatch` gets every incident as `Added`, then one `Bookmark`; with none, the `Bookmark` alone.
- An occurrence that starts or ends arrives as one `Modified` of its incident. An open incident whose signal holds sends none.
- Deleting a cluster delivers `Deleted` for each of its incidents, though only `KeyClusters` was notified.
- A subscriber to `incidentOccurrenceEventFeed` gets the retained occurrences newest first, then one `Bookmark`; an occurrence that ends arrives again under its id with its `endedAt`.
- `incidentOccurrenceEventFeed` for an unknown id sends the `Bookmark` alone.
- A read that fails ends either subscription with a `subscriptionFailed` frame.

## When it lands

- `apps/desktop/docs/read-contracts/graphql.md`'s *Field names* table says an event feed's frame is `EventFeedFrame` for a cluster `Event` and `<Noun>EventFeedFrame` for any other element, which reuses `EventFeedFrameType`.
