# src/components/monitoring — what monitoring found

The Incidents dialog and `MonitorTargetStatus`. Also owns `src/lib/incidents.tsx`.

**One provider holds the reads**: `IncidentsProvider`, mounted in `AppLayout` above the bar and the dialogs, holds `monitorTargetCollectionWatch`, `incidentCollectionWatch` and `monitorSignalList`, so the bell and the dialog read the same maps. Its hooks return null until both `Bookmark`s and the clusters list land: draw nothing before. `useMonitorTargets()` leaves out a target whose cluster `useClusters()` does not list, which drops one awaiting deletion before its `Deleted` arrives; a target with no `clusterID` is always listed. `useIncidents(targetID?)` holds only those targets' incidents, in the list's order.

- **The webview knows an integration in one place**, `INTEGRATION_LABEL`. Read errors and end reasons are `Record`s over their enums, so a new value fails the build. A signal's label is joined from `useMonitorSignals()` by name.
- **Every string a target returned goes through `VisibleText`**: target and subject labels, each attribute's key and value. A signal label is the sidecar's fixed text.
- **A target's state is a line above its incidents, never in place of them** (`MonitorTargetStatus`, the first of off, failed, never read, partial). Off says off alone, and links to Settings' Monitoring tab.
- **The dialog's filter and selection are its own state**, chosen once when the lists land: the incident it opened on (`openDialog('incidents', id)`) and its target, else the window's cluster's target, else *All targets*. A filter or selection whose target or incident leaves the lists is dropped in the render that sees it, which unmounts the detail and cancels its feed. A picked filter that leaves the selection out clears it. The dialog holds the last lists through a reconnect (`useLastSettled`), so what the user picked survives it, but drops at once a target whose cluster `useClusters()` drops. Its panes are keyed on `openSeq`, so each open chooses afresh.
- **The detail's feed is unshared and tagged with its incident**: a frame for another, a late `Bookmark` included, is dropped (`useIncidentOccurrenceEventFeed`). Occurrences are ordered by `number`, never by time.
- *Resolved* is said only for `lastEndReason` `Resolved`: nobody saw a `Stopped` or `Stale` incident recover.

The dialog's look is built from the Settings dialog's frame and tokens; the design library has no screen for it yet.
