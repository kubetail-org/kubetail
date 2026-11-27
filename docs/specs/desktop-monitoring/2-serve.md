# Step 2 — Serve

Prerequisite: step 1 on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The webview can read incidents and their occurrences. No screen draws them yet.

## Design

Each read takes its contract from [`apps/desktop/docs/read-contracts/`](../../../apps/desktop/docs/read-contracts/README.md).

| Read | Contract | Go | GraphQL |
| --- | --- | --- | --- |
| Incidents | Collection watch | `WatchIncidentCollection` | `incidentCollectionWatch(clusterID: ClusterID)` |
| An incident's occurrences | List | `ListIncidentOccurrences` | `incidentOccurrenceList(incidentID: IncidentID!)` |

- **`incidentCollectionWatch`** takes a cluster or, with none, every cluster's. The service listens on `KeyIncidents`, re-reads and diffs, as the chat list does. An incident deleted with its cluster, or by retention, arrives as `Deleted`.
- **`Incident`** carries its id, `clusterID`, the subject's group, kind, namespace and name, `signal` (an enum of the five), `baseline`, `firstSeenAt`, `lastSeenAt`, `open` and `occurrenceCount`. The count and the two times of its newest occurrence ride on the record, so the list draws without a second read and a new occurrence arrives as `Modified`.
- **`incidentOccurrenceList`** answers the newest 100, newest first. The webview asks again when the incident's `occurrenceCount` or `open` moves.
- **`IncidentID`** is a scalar of its own, as `ChatID` and `MemoryID` are.

The per-cluster switch is already served: `Cluster.monitoringEnabled` and `clusterMonitoringEnabledSet`.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Service reads | Planned | — |
| 2. Schema and resolvers | Planned | — |

1. **Service reads.** The two methods on the monitor service, the watch built on the notice.
2. **Schema and resolvers.** A `# --- Incidents ---` section in `schema.graphqls`, `make generate-desktop`, the resolvers over `subscriptionStream`, and resolver tests through `graph.NewServer`.

## Acceptance

- A subscriber gets every incident as `Added`, then one `Bookmark`.
- An occurrence that starts or ends arrives as one `Modified` of its incident.
- A watch scoped to one cluster carries no other cluster's incident.
- Deleting a cluster delivers `Deleted` for each of its incidents.
- `incidentOccurrenceList` for an unknown id answers not-found.

## When it lands

- `apps/desktop/docs/read-contracts/graphql.md`'s subject table gains the incidents row.
