# Step 3c — Settings

Prerequisite: step 3a on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The Settings dialog's Monitoring tab (`settings-dialog/monitoring.tsx`) replaces its *Coming soon* placeholder with what monitoring does and a switch per target.

## Design

**The tab opens with one sentence on what is watched and when**: each enabled target, for the signals by their `label` from `useMonitorSignals()`, in the list's order, while Kubetail is running, the tray included; a quit app watches nothing. `useMonitorSignals()` is empty until the list answers, and the sentence then names no signal.

**A row per target** from `useMonitorTargets()`, which already sorts by label and leaves out a target whose cluster is gone: its label and a switch. Rows that share a label, as two clusters of one name do, each add their cluster's kube context under it and to their switch's name. Nothing draws while it returns null, before both watches' `Bookmark`s and the clusters list. With no target, the tab says so.

**The switch writes through `useMonitorTargetEnabledSet()`**, a hook in `src/lib/incidents.tsx` beside the reads, holding the `MonitorTargetEnabledSet` document over `monitorTargetEnabledSet`, as `useToolSettings()` holds the Tools tab's: the targets in flight, each target's last refusal (`refusalOf`), and `set(id, enabled)`. Its one caller is this tab, so it joins the module that owns targets rather than a file of its own. `IncidentsProvider` holds that state, so a switch in flight and its refusal outlive a change of tab.

**The switch shows the watch's `enabled`, never its own press.** A press sends the mutation and disables that row's switch until the mutation answers; a second press on a target in flight sends nothing. The switch moves when the target's `Modified` arrives, which also moves it for a change made in another window. A mutation that fails leaves the switch where the watch has it and says why on the row, until that row's next press; `errorReportExchange` logs it as it logs every operation error.

**A row says why nothing is detected.** A target that is on draws step 3a's `MonitorTargetStatus` under its label: *cannot be read* for `Failed`, *has not been read yet* for `NeverRead`, *some signals cannot be judged* for `Partial`. A target that is off draws none, since the switch already says it and that line's link opens this tab: so a target switched off while `Failed` shows no read reason.

**The tab is opened by its label**: `openDialog('settings', 'Monitoring')`, from the off line, takes a tab's label as a plain string, so the tab keeps the label `Monitoring`.

**Every string from a target is drawn through `VisibleText`**: its label, here and in `MonitorTargetStatus`.

Steps 3b and 5 each add a section to this tab: notification permission and automatic triage.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. The tab | In review | — |

1. **The tab.** `useMonitorTargetEnabledSet()` and its tests in `src/lib/incidents.test.tsx`; `settings-dialog/monitoring.tsx` and its test, with the placeholder comment removed.

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
- `apps/desktop/src/components/monitoring/CLAUDE.md` names `useMonitorTargetEnabledSet()` among the hooks `src/lib/incidents.tsx` holds.
