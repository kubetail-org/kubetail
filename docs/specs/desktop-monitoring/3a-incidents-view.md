# Step 3a — Incidents view

Prerequisite: step 2 on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The user can see what monitoring found: a list of the window's cluster's incidents, one incident with its occurrences, and the open incidents of every cluster under the app bar's bell.

## Design

**A third route, `/incidents`**, under the `_app` layout beside Chat and Dashboard, with an entry in the left sidebar. The selected incident is the `incident` search param, so each window keeps its own.

**One provider holds the one `incidentCollectionWatch`**, mounted with the app shell, so the list and the bell read the same map and neither joins a shared stream late. The list is the window's cluster's incidents, joined by `clusterID`: open incidents first, newest `lastStartedAt` first, then recovered ones, newest `lastSeenAt` first. A row shows the subject's kind and namespaced name, the signal's label, *Open since* its `lastStartedAt` or *Recovered, last seen* its `lastSeenAt`, and the occurrence count. Nothing draws before the `Bookmark`.

**The empty state says why.** One of: nothing found; monitoring is off for this cluster (`Cluster.monitoringEnabled`), with a link to the Monitoring tab; the cluster's cache is not trusted yet, so nothing can be detected. Trust is read from `clusterCacheHealthKeyedStateWatch`, joined onto the cluster's active cache by `cacheID`, and only once that cache is in the cache collection.

**The detail** shows the same facts and the occurrences from `incidentOccurrenceEventFeed`, folded with `eventFeedReducer` (newest `startedAt` first, capped at 100) and subscribed unshared, each with its start and its duration, or *ongoing*. When `occurrenceCount` is above 100, it says it shows the newest 100 of that count. Nothing draws before its `Bookmark`. A frame for another incident is dropped. When the incident's `Deleted` arrives, the detail closes.

**The bell** (`shell/app-bar/notification-button.tsx`) lists every cluster's open incidents, newest first, each naming its cluster, and shows their count on the button. Pressing one opens it: in this window when it is the window's cluster, else in a window on that cluster.

**Every string from a cluster is drawn through `VisibleText`**: the subject's kind, namespace and name. A signal's label is the webview's own, a `Record` over the enum, so a new signal fails the build.

**New code**: `src/lib/incidents.tsx` (the hooks), `src/components/monitoring/` (the list, the detail and their guide), `src/routes/incidents.tsx`.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Hooks | Planned | — |
| 2. Route, list and detail | Planned | — |
| 3. Bell | Planned | — |

1. **Hooks.** `IncidentsProvider` folding `IncidentCollectionWatch` with `collectionWatchReducer`; `useIncidents(clusterID?)`, named for what it returns, and `useIncidentOccurrenceEventFeed(incidentID)`, named for its operation.
2. **Route, list and detail.** The route, the sidebar entry, the three empty states.
3. **Bell.** The list, the count and the press.

## Acceptance

- An incident that opens while the list is shown appears without a reload, and moves to *Recovered* when its occurrence ends.
- A subject name holding a bidi override or an escape sequence is drawn as visible text.
- With monitoring off for the cluster, the list is replaced by the reason.
- The bell counts an open incident on another cluster.
- Two windows on two clusters each show their own cluster's list.
- The list and the bell show nothing before the `Bookmark`, and the bell mounted before the route shows the same incidents the route does.
- Switching the detail to another incident draws none of the first one's occurrences.

## When it lands

- `apps/desktop/src/components/monitoring/CLAUDE.md`, with its `AGENTS.md` symlink and a budget in `.github/ci-config/claude-md-budgets.yml`.
- `apps/desktop/src/components/shell/CLAUDE.md` says what the bell lists.
- `apps/desktop/design/` maps the new screen once the design library has it.
