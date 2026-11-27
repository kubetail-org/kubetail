# Step 3a — Incidents view

Prerequisite: step 2 on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The user can see what monitoring found: a list of every target's incidents, and one incident with its occurrences, in a dialog the app bar's bell opens. The bell itself lists the open incidents.

## Design

**An `incidents` dialog** holds the list and one incident's detail side by side, registered in `AppDialogs` beside Settings. The bell opens it: the sidebar has no entry and there is no route. `openDialog('incidents', incidentID)` takes the incident to open on, as `openDialog('clusters', detail)` takes the Clusters dialog's row, and `openDialog('settings', tab)` takes the Settings tab to open on; each open clears what the last one named. An id that `useIncidents()` does not hold once both `Bookmark`s have arrived opens the dialog with no incident selected. The selected incident and the target filter are the dialog's own state, so each window keeps its own, and both start over each time it opens. It opens filtered to its window's cluster's target, or to the incident's target when it opens on one; the filter offers *All targets* and each target by its label. A window with no cluster's target opens on *All targets*.

**One provider holds the one `incidentCollectionWatch`, the one `monitorTargetCollectionWatch` and the result of `monitorSignalList`**, mounted with the app shell (`AppLayout`), so the list and the bell read the same maps and neither joins a shared stream late. The dialog mounts only while it is open, and reads the same maps. `useMonitorSignals()` returns the signals by name. Each watch's frame carries its item as `target` or `incident`, and a frame whose item is null is dropped: an `integration` the resolver cannot map nulls the record.

**The provider's hooks do the joins the watches leave to their consumer.** `useMonitorTargets()` returns each target under one rule: a target with a `clusterID` is returned only while that cluster is in `useClusters()`'s list, whose fold already drops a cluster carrying `deletionRequestedAt`; a target without one is always returned. A cluster's target can outlive its cluster by one reconcile, and the rule hides it for that moment. A target's name is its own `label`. `useIncidents(targetID?)` returns the incidents of the targets `useMonitorTargets()` returns.

**The list** is the filter's incidents: open incidents first, by the incident's own `severity`, `Critical` before `Warning` before `Info`, then newest `lastStartedAt`; then ended ones, newest `lastSeenAt` first. A row shows the target's label and integration, the subject's label, the signal's label, the occurrence count, and its state: *Seen since* its `lastStartedAt`, which is when Kubetail first saw the failure and may be later than when it began; *Resolved, last seen* its `lastSeenAt` when `lastEndReason` is `Resolved`; *Last seen* its `lastSeenAt` otherwise, since nobody saw a `Stopped` or `Stale` incident recover. An incident whose `targetUID` is not its target's `currentUID`, once that is set, is marked *On a server this target no longer reaches*. Nothing draws before both `Bookmark`s and the cluster watch's.

**The webview knows an integration in one place**: its label, a `Record` over `MonitorIntegration`. A target, a subject and a signal are each drawn from the label the sidecar sends, so a new integration fails the build there and nowhere else.

**A target's state is a line above its incidents, never in place of them.** A target keeps the incidents it has through a switch-off and through a read that fails, so the list and the selected detail stay. For one target the line is the first of these that holds:

- monitoring is off for this target (`enabled` false), with a link to Settings' Monitoring tab;
- the target cannot be read, from `readState` `Failed` and the reasons in `unknownSignals`: *still loading*, *switched off*, *access denied*, *unreachable* and the rest, each a `Record` over `MonitorReadError`;
- the target was never read, from `readState` `NeverRead`;
- some signals cannot be judged, from `readState` `Partial`: each one and why, from `unknownSignals`, its label joined from `useMonitorSignals()` by name.

A target that is off keeps the `readState` of its last reading, and its line says off alone. A target that is on and `Ok` has no line. *All targets* shows a line per target that has one. These lines are `MonitorTargetStatus` in `src/components/monitoring/`, which the bell and step 3c's tab draw too. A filter with no incidents says *Nothing found* under its lines.

**A filter or a selection whose target leaves `useMonitorTargets()` is dropped at once.** The filter falls back to *All targets*, the selection clears and the detail cancels its feed. A cluster's delete hides its target before the incident's `Deleted` arrives, and nothing waits for that frame.

**The detail** shows the same facts, the incident's `subjectAttributes` drawn as key–value text, and the occurrences from `incidentOccurrenceEventFeed`, folded with `eventFeedReducer` from each frame's `occurrence` (highest `number` first, capped at 100) and subscribed unshared, each with its `number`, its start and its duration, or *ongoing*, how it ended (`endReason`), and its `detail` drawn as key–value text. When `occurrenceCount` is above 100, it says it shows the newest 100 of that count. Nothing draws before its `Bookmark`. A frame for another incident is dropped by its `incidentID`, a `Bookmark` included, so the detail stays loading until its own arrives. When the incident's `Deleted` arrives on `incidentCollectionWatch`, the detail closes and cancels the feed, which sends nothing for a deletion.

**The bell** (`shell/app-bar/notification-button.tsx`) lists every target's open incidents, newest first, each naming its target, and shows their count on the button. Pressing one opens the dialog on it, in this window. Below the list, *View all* opens the dialog with no incident selected, and is there with no open incident too. With no open incident the bell says so, above the line of each target that is off or cannot be read.

**Every string from a target is drawn through `VisibleText`**: the target's label, the subject's label, and each key and value of `subjectAttributes` and `detail`. A signal's label is the sidecar's fixed text, `signalLabel`.

**New code**: `src/lib/incidents.tsx` (the hooks and the documents `MonitorTargetCollectionWatch`, `IncidentCollectionWatch`, `MonitorSignalList` and `IncidentOccurrenceEventFeed`), `src/components/monitoring/` (the dialog, the list, the detail, the filter and their guide). `DialogId` in `src/lib/dialog.tsx` gains `incidents`. `monitoring/` is an area folder of its own, between `logs` and `settings` in the import order, since step 3c's tab draws `MonitorTargetStatus` and the bell in `shell` opens the dialog. `MonitorSignalList` is `monitorSignalList`'s first caller, so `TestEveryQueryFieldHasACaller` drops the exception step 2 wrote for it.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Hooks | Planned | — |
| 2. Dialog, list and detail | Planned | — |
| 3. Bell | Planned | — |

1. **Hooks.** `IncidentsProvider` folding both watches with `collectionWatchReducer`, each frame told by its `type`, and holding the signal list; `useIncidents(targetID?)`, `useMonitorTargets()` and `useMonitorSignals()`, named for what they return, with their joins, and `useIncidentOccurrenceEventFeed(incidentID)`, named for its operation.
2. **Dialog, list and detail.** The dialog and its registration, the incident `openDialog` takes, the filter, `MonitorTargetStatus`.
3. **Bell.** The list, the count, the press, *View all* and the empty state.

## Acceptance

- An incident that opens while the list is shown appears without a reload, and moves to *Resolved* when its occurrence resolves.
- A subject label, or a `subjectAttributes` or `detail` value, holding a bidi override or an escape sequence is drawn as visible text.
- Switching a target's monitoring off with one of its incidents selected keeps the list and the detail, shows the occurrence as `Stopped`, and adds the reason above the list.
- A target whose `readState` turns `Failed` keeps its incidents listed under the reason.
- A target switched off while `Failed` or `Partial` says monitoring is off, and nothing of the read.
- A filter with no incidents says *Nothing found*, under the reason when its target has one.
- A target whose `readState` is `Failed` with `PermissionDenied` says so above its incidents.
- A target whose `readState` is `Partial` names its unknown signals above its incidents.
- An incident that opened on the server a context reached before is marked, and one that ended as `Stale` reads *Last seen*, never *Resolved*.
- A target with no `clusterID` is listed whatever the cluster watch holds.
- A target whose cluster has left the cluster watch is left out before its own `Deleted` arrives.
- With that target filtered and one of its incidents selected, and the incident's `Deleted` held back, the filter falls to *All targets*, the selection clears and the feed is cancelled.
- The bell counts an open incident on another target.
- Two windows on two clusters each open the dialog filtered to their own cluster.
- Pressing an incident in the bell opens the dialog on it, filtered to its target, whichever cluster the window is on.
- The dialog opened on an id no incident has shows the window's filter and no selection.
- *View all* opens the dialog from a bell with no open incident, and that bell names a target that is off.
- The dialog reopened after a close has no incident selected and its window's filter.
- Closing the dialog cancels the occurrence feed.
- A cluster whose delete is requested loses its target from the filter and its incidents from the list and the bell at once, before any `Deleted` arrives.
- Renaming a cluster renames its target in the list, from the target's `Modified`.
- The list and the bell show nothing before both `Bookmark`s, and the dialog opened later shows the same incidents the bell does.
- Switching the detail to another incident draws none of the first one's occurrences, and a late `Bookmark` from the first leaves the second loading.
- Two occurrences whose `startedAt` are out of order are listed by `number`.

## When it lands

- `apps/desktop/src/components/monitoring/CLAUDE.md`, with its `AGENTS.md` symlink and a budget in `.github/ci-config/claude-md-budgets.yml`.
- `apps/desktop/src/components/shell/CLAUDE.md` says what the bell lists and the dialog it opens.
- `apps/desktop/src/CLAUDE.md` lists `monitoring/` among the area folders, in the import order, and its guide in the table.
- `apps/desktop/design/` maps the new dialog once the design library has it.
