# Step 3c — Settings

Prerequisite: step 3a on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The Settings dialog's Monitoring tab replaces *Coming soon* with what monitoring does and a switch per target.

## Design

**The tab opens with one sentence on what is watched and when**: the signals, by their labels from `useMonitorSignals()`, while Kubetail is running, the tray included.

**A row per target** from `useMonitorTargets()`, sorted by label: its label and a switch over `monitorTargetEnabledSet`, through a new `src/lib/monitor-mutations.ts` as `src/lib/cluster-mutations.ts` holds the Clusters tab's. Nothing draws before the `Bookmark`.

**The switch shows the watch's `enabled`, never its own press.** A press sends the mutation and disables that row's switch until the mutation answers; the switch moves when the target's `Modified` arrives. A mutation that fails leaves the switch where the watch has it and says so on the row. A change made in another window moves the switch the same way.

**A row says why nothing is detected.** A target whose `readState` is `Failed` keeps its switch, beside the reason. That line, and the ones for `Partial` and `NeverRead`, are step 3a's `MonitorTargetStatus`. The tab leaves out its *monitoring is off* line, which the switch already says, so a target that is off has no line here.

Steps 3b and 5 each add a section to this tab: notification permission and automatic triage.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. The tab | Planned | — |

1. **The tab.** `settings-dialog/monitoring.tsx` and its test, with the placeholder comment removed.

## Acceptance

- A switch turned off ends the target's open occurrences (step 1) and the incidents dialog shows the reason (step 3a).
- A cluster added while the tab is open appears with its switch on.
- A switch pressed twice before its mutation answers sends one mutation.
- A mutation that fails leaves the switch at the watch's value and shows the error on its row.
- A switch turned off in another window moves here, from the target's `Modified`.
- A target switched off while its `readState` is `Failed` shows no read reason on its row.
- A target's label is drawn through `VisibleText`.

## When it lands

- `apps/desktop/src/components/settings/CLAUDE.md` gains a **Monitoring** line.
