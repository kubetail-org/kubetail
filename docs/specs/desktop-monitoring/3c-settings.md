# Step 3c — Settings

Prerequisite: step 2 on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The Settings dialog's Monitoring tab replaces *Coming soon* with what monitoring does and a switch per cluster.

## Design

**The tab opens with one sentence on what is watched and when**: the five signals, while Kubetail is running, the tray included.

**A row per cluster** from `useClusters()`, with its name and a switch over `clusterMonitoringEnabledSet`, through `src/lib/cluster-mutations.ts` as the Clusters tab's switches are. Nothing draws before the `Bookmark`. A cluster whose cache cannot be trusted keeps its switch and says nothing is detected until it syncs.

Steps 3b and 5 each add a section to this tab: notification permission, and automatic triage.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. The tab | Planned | — |

1. **The tab.** `settings-dialog/monitoring.tsx` and its test, with the placeholder comment removed.

## Acceptance

- A switch turned off ends the cluster's open occurrences (step 1) and the incidents view shows the reason (step 3a).
- A cluster added while the tab is open appears with its switch on.
- A cluster's name is drawn through `VisibleText`.

## When it lands

- `apps/desktop/src/components/settings/CLAUDE.md` gains a **Monitoring** line.
