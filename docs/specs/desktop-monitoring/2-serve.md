# Step 2 — Serve

Prerequisite: step 1 on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The webview can read targets, signals, incidents and their occurrences, and switch a target's monitoring. No screen draws them yet.

## Design

Each read takes its contract from [`apps/desktop/docs/read-contracts/`](../../../apps/desktop/docs/read-contracts/README.md).

| Read | Contract | Go | GraphQL | Frame |
| --- | --- | --- | --- | --- |
| Targets | Collection watch | `WatchMonitorTargetCollection` | `monitorTargetCollectionWatch` | `MonitorTargetCollectionWatchFrame` |
| Signals | List | `ListMonitorSignals` | `monitorSignalList` | — |
| Incidents | Collection watch | `WatchIncidentCollection` | `incidentCollectionWatch` | `IncidentCollectionWatchFrame` |
| An incident's occurrences | Event feed | `SubscribeIncidentOccurrenceEventFeed` | `incidentOccurrenceEventFeed(incidentID: IncidentID!)` | `IncidentOccurrenceEventFeedFrame` |

Neither watch nor the feed has a query beside it: each delivers its whole state first, and no caller reads once and leaves.

Enum values are PascalCase, as the schema's are.

### Targets

- **`monitorTargetCollectionWatch` carries every target** and takes no scope. The collection is the targets of the integrations this build registers, a set fixed for the life of the process: a stored row of any other integration is no item of it.
- **`MonitorTarget`** carries its `id` (`MonitorTargetID`, a scalar of its own), `integration` (`MonitorIntegration`, an enum whose one value is `Kubernetes`), `label`, `clusterID` (nullable, set for a cluster's), `currentUID` (nullable until the first reading), `enabled`, `readState` (`NeverRead`, `Ok`, `Partial` or `Failed`) and `unknownSignals`, each a `MonitorUnknownSignal`: the signal's `name` and its `reason` (`MonitorReadError`, an enum of the codes step 1 lists). The entry carries no label: the webview joins it from `monitorSignalList` by name, since a collection never embeds another's fields.
- **`readState` and `unknownSignals` are derived against the signals this build registers.** A stored entry naming a signal no registration holds is left out, so every entry served has a label in `monitorSignalList`. The state is `Failed` when every registered signal of the integration is named. A signal registered since the target's last reading is in no stored entry, and counts as judged until the next reading writes the map again. A target not yet read says so in the value, `NeverRead`, and is never null.
- **`clusterID` is resolved, not stored.** The resolver fills it from the target's key through `kubesignals.ClusterID`, and it is null for a target of any other integration.
- **The watch diffs what it serves, not what is stored.** The service's record holds each served value in a comparable form: the key, and the unknown signals as the canonical JSON of the entries left after the retired ones are dropped, sorted by name, which the resolver decodes. A stored map that changes only in a retired signal's entry leaves the record equal, and sends no frame.
- **A target carries its own label**, which its integration wrote: a cluster's is its name, rewritten by the reconcile when the cluster is renamed, and delivered as a `Modified`.
- **The watch never filters.** The target of a cluster awaiting deletion stays an ordinary item until the reconcile that follows the cluster's delete removes its row, and then arrives as `Deleted`. The cluster's record carries the mark, `deletionRequestedAt`, and the webview reads it there.
- **The service listens on `KeyMonitorTargets` before its first read**, then re-reads and diffs. Every write to a field it serves notifies that key: the reconcile, the switch, a UID, the unknown signals and the first reading.
- **`monitorTargetEnabledSet(id: MonitorTargetID!, enabled: Boolean!): MonitorTarget!`** sets the switch in step 1's one transaction, which also ends the target's open occurrences when it turns monitoring off. It commits, notifies `KeyMonitorTargets` and `KeyIncidents`, and returns the target as the watch then delivers it. `Cluster.monitoringEnabled` and `clusterMonitoringEnabledSet` are removed.

### Signals

- **`monitorSignalList` answers every registered signal** as a `MonitorSignal`: its `name`, `integration`, `label` and `severity`. The list is fixed for the life of the process, so it is a List with no watch beside it, and nothing polls it. The webview keeps it by name and joins labels from it.
- **A signal is a `String` wherever a record names one**, with its label beside it. No enum lists the signals: a screen draws the label it is sent, and a new signal changes no schema.

### Incidents

- **`incidentCollectionWatch` carries every target's incidents**, takes no scope and never filters: its items are the incidents of the targets `monitorTargetCollectionWatch` carries, and those of a cluster awaiting deletion stay until they are deleted with its target. The webview joins them onto targets by `targetID`. One watch serves both the dialog's list and the bell.
- **The service listens on `KeyIncidents` and `KeyMonitorTargets` before its first read**, then re-reads and diffs, as the memory stream does. A target's delete cascades to its incidents and notifies `KeyMonitorTargets` alone. An incident deleted with its target, or by retention, arrives as `Deleted` with its last-known state.
- **`Incident`** carries its id, `targetID`, `targetUID`, `subjectLabel`, `subjectAttributes` (each a `MonitorAttribute`, a `label` and a `value`, as `RequestDetail` is, sorted by label), `signal`, `signalLabel`, `severity`, `firstSeenAt`, `open`, `occurrenceCount` (every occurrence it has had, the trimmed ones included), `lastStartedAt` (the start of its newest occurrence, the one with the highest `number`), `lastSeenAt` (null while it is open) and `lastEndReason` (`Resolved`, `Stopped` or `Stale`; null while it is open). The count and the times ride on the record, so the list draws without a second read, and a new or ended occurrence arrives as `Modified`. An open incident's last-seen time moves with every reading and is not served, so a signal that holds sends no frame. Each field of the service's record is compared by value, so the diff's `==` holds: the record holds the attributes as their stored JSON, which the resolver decodes, and a nullable time as a value and a flag, never a pointer, which two reads of one unchanged row would hold unequal.
- **The schema has no type for an integration's subject.** `subjectAttributes` are the integration's own labels and values, which the webview draws as key–value text, as it draws an occurrence's `detail`.
- **`signalLabel` and `severity` are the incident's own stored fields**, copied when it opened, not the signal's embedded: they outlive a signal a later build drops.
- **`IncidentID`** is a scalar of its own, as `ChatID` and `MemoryID` are.

### Occurrences

An incident's occurrences are a retained history: the newest 100 by `number`, the oldest trimmed unannounced. That makes them an event feed.

- **The snapshot is the retained occurrences, highest `number` first, then one `Bookmark`.** Each later frame is an occurrence that started or ended, sent under its id; an ended one is sent again with its `endedAt`.
- **`IncidentOccurrenceEventFeedFrame`** is `type: IncidentOccurrenceEventFeedFrameType!`, `Occurrence` or `Bookmark`, `incidentID: IncidentID!`, and `occurrence: IncidentOccurrence`, null on `Bookmark`. The shared `EventFeedFrameType` names its event `Run`, so this feed has a type of its own. `IncidentOccurrence` carries its `id` (`IncidentOccurrenceID`, a scalar of its own, as `MessageID` is), its `incidentID`, its `number`, `startedAt`, a nullable `endedAt` with its `endReason`, and its `detail`, each a `MonitorAttribute`. The webview draws `detail` as key–value text, never as markup.
- **The frame carries its scope, on the envelope.** `IncidentOccurrenceEventFeedFrame.incidentID` is set on every frame, the `Bookmark` included, whose `occurrence` is null. The webview drops a frame whose `incidentID` is not the one now subscribed to, so a late `Bookmark` from the incident shown before leaves the current one loading.
- **A subscription sends exactly one `Bookmark`, after its snapshot.** An id that names no incident sends an empty snapshot and that `Bookmark`. So does the id of a stored incident whose integration this build lacks: the feed serves no incident the incident watch leaves out.
- **A deletion sends nothing.** An event feed has no `Deleted`: once its incident is deleted the feed stays open and silent, and `incidentCollectionWatch` delivers the `Deleted`. The webview cancels the feed when it arrives.
- The service listens on `KeyIncidents` before it reads, and on each notice re-reads the incident's occurrences and sends those that are new or changed. It listens on nothing else: a target's delete notifies `KeyMonitorTargets` alone and leaves this feed nothing to send.

### All of them

- **Each subscription's Go method returns the monitor package's own `*Stream[T]`**, as `memory` and `chat` each have one, and each resolver goes through `subscriptionStream`, so a failed read ends the subscription with a `subscriptionFailed` frame.
- **Each field's description opens with its contract**: "A collection watch of every monitor target …", "A List of every signal …", "A collection watch of every target's incidents …", "An event feed of one incident's occurrences …".

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Service reads | Planned | — |
| 2. Schema and resolvers | Planned | — |

1. **Service reads.** The four methods and the switch on the monitor service, each subscription listening before it reads.
2. **Schema and resolvers.** A `# --- Monitoring ---` section in `schema.graphqls`, the removal of the cluster's field and mutation, `make generate-desktop`, the resolvers over `subscriptionStream`, and resolver tests through `graph.NewServer`.

## Acceptance

- A subscriber to `monitorTargetCollectionWatch` gets every target as `Added`, each with its label and its cluster's id, then one `Bookmark`; with none, the `Bookmark` alone. Renaming a cluster sends one `Modified` with the new label.
- A target of an integration other than Kubernetes carries a null `clusterID`.
- Requesting a cluster's delete sends no frame for its target or its incidents; each arrives as `Deleted` once the reconcile has removed the target's row, though only `KeyMonitorTargets` was notified.
- A stored target whose integration this build lacks, and each of its incidents, is no item of either watch, which serve the rest (`TestAnUnknownIntegrationFailsNoWatch`).
- An incident whose stored signal no registration names is served with its stored name, label and severity.
- `monitorTargetEnabledSet` off ends the target's open occurrences as `Stopped` before it returns, each arriving as a `Modified` of its incident, and returns the target with `enabled` false.
- A target whose reading goes from judging everything to judging nothing arrives as one `Modified` with `readState` `Failed` and every signal in `unknownSignals`, each with its reason.
- With its first reading held back, a new target arrives as `Added` with `readState` `NeverRead`; the reading then arrives as one `Modified`.
- A target whose UID changes arrives as one `Modified` with the new `currentUID`, though the fold's other writes notify `KeyIncidents`.
- `monitorSignalList` answers Kubernetes's six signals, each with its label and severity; a monitor with no registration answers an empty list.
- A subscriber to `incidentCollectionWatch` gets every incident as `Added`, then one `Bookmark`; with none, the `Bookmark` alone.
- With each write held until the watch has re-read, an occurrence that starts or ends arrives as one `Modified` of its incident, an ended one with its `lastEndReason`. An open incident whose signal holds sends none.
- Writes that land between two re-reads arrive as one frame with the last state: a target inserted and read before the watch re-reads arrives as one `Added` already `Ok`, and an occurrence that ends while the next starts arrives as one `Modified` of an open incident with the later `lastStartedAt`.
- A subscriber to `incidentOccurrenceEventFeed` gets the retained occurrences, highest `number` first, then one `Bookmark`; an occurrence that ends arrives again under its id with its `endedAt` and `endReason`.
- `incidentOccurrenceEventFeed` for an unknown id sends the `Bookmark` alone, carrying the id asked for. So does a subscription by the id of a stored incident whose integration this build lacks, though it has occurrences.
- An incident deleted after its feed's `Bookmark`, by its target's delete or by retention, sends no frame on the feed, and no second `Bookmark`.
- A target whose stored `unknown_signals` names a signal this build does not register is served without that entry, and reads `Failed` when the entries left name every registered signal.
- An incident on a Deployment carries the `subjectLabel` `Deployment default/web`, the `signalLabel` `Crash loop`, its server's UID as `targetUID`, and `subjectAttributes` of its group, kind, namespace and name.
- Two reads of an unchanged incident whose `lastSeenAt` is set send no frame, and nor does a change to a target's stored entry for a signal this build does not register (`TestTheWatchesDiffServedValues`).
- A read that fails ends any of the subscriptions with a `subscriptionFailed` frame.

## When it lands

- `apps/desktop/docs/read-contracts/graphql.md`'s *Field names* table says an event feed's frame is `EventFeedFrame` for a cluster `Event` and `<Noun>EventFeedFrame`, with a `<Noun>EventFeedFrameType` of its own, for any other element.
