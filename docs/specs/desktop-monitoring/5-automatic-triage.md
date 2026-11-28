# Step 5 — Automatic triage

Prerequisites: steps 3b, 3c and 4 on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The user can leave triage on. Each new incident is then triaged unasked, and only one that needs attention alerts.

## Design

**One setting, `monitoring.triage.model`**, a row of `settings` for every target: a provider, a model and an effort as JSON, or unset. Set means automatic triage is on. It is an `Entry` of `services/settings` (`monitoring.go`) whose check refuses an empty provider or model, and its default and strictest value are unset, so a stored value Kubetail cannot use reads as off. Its write cancels queued triages in the same transaction, so the monitor reads and writes the row on its own transactions, through the statement text and the encoding `monitoring.go` exports, as `tools.go` exports the tools' settings: no other package spells the row.

**The user sets it in two places**: a checkbox beside *Triage with AI*, *Always triage new incidents with this model*, and a section of the Monitoring tab with the model and a way to turn it off. The checkbox turns the setting on and never off, and commits with the triage: `incidentTriage` gains `always: Boolean`, `Monitor.Triage` an `always bool`, and with it true `store.queueTriage` writes the setting and queues the triage in one transaction. A submission the mutation refuses, as for an incident already queued, leaves the setting as it was. Ticking the box writes nothing, and closing the picker leaves the setting as it was. While the setting already names the picked model, the checkbox gives way to a line saying new incidents are triaged with it, linked to the tab (`openDialog('settings', 'Monitoring')`). The tab states that this spends model calls while nobody is watching, and that at most 6 triages start per target per hour. The number is the sidecar's constant and is written again in the tab's text, and a comment on each names the other.

**The setting is a state watch**, `Monitor.WatchTriageModelState` and `monitoringTriageModelStateWatch`, from [`apps/desktop/docs/read-contracts/`](../../../apps/desktop/docs/read-contracts/README.md). It switches spending on for every window, so each window shows its value as it is, not as that window last wrote it. The first frame is sent at once, and unset is null, its empty value and no error. It listens on a new key, `appdb.KeySettings`, before it reads, and `monitoringTriageModelSet` (`Monitor.SetTriageModel`, null to unset) and a ticked `incidentTriage` notify that key after their commit. A model with an empty provider or model id is refused `KUBETAIL_VALIDATION_ERROR` through `monitorErr`. The webview's hooks are in `src/lib/incidents.tsx`; both the checkbox and the tab fold the watch with `stateWatchReducer`.

**With the setting on, an occurrence that starts queues a triage of its incident** when it is not `baseline` and is one of:

- the incident's first ordinary occurrence, the one that opens it or clears its `baseline`, which alerts under step 3b;
- the incident's third occurrence, the one numbered 3, with the brief saying so.

One occurrence that is both queues one triage. The triage is queued in the fold's transaction (`store.commit`, `writeStart`), after the occurrence's insert so the brief carries its detail, with the setting's model read in that transaction. After the commit the loop hands the request to the per-target queue, as `Monitor.Triage` does. `incidents` gains `triage_trigger`, `manual` or `automatic`, NULL while `triage_state` is `none`, which says who asked for the incident's triage. An integration with no `Triager` queues nothing, and its incidents alert as step 3b has it.

**An occurrence whose incident already has a triage queued queues nothing.** A queued automatic triage answers for it. A queued manual one does not: the user asked for it and reads its report, so the incident alerts at once, untriaged.

**An automatic triage is authorized when its run starts, not when it is queued.** `store.admitTriage` already runs on `RunMonitor`'s insert transaction (`sqlstmt.Set.On(tx)`) and links the run only while the incident holds the request (`triage_request_id`) and the target is on under the request's generation, which a cancelled request fails though the incident has been queued again since. For an automatic triage it then:

- trims the rows of `monitor_triage_starts` past an hour;
- counts the target's rows, and refuses at 6, as `cap_reached`;
- inserts the triage's row.

A refusal rolls back the run and the row with it, so a refused triage takes no slot, and a run that is inserted is counted whatever happens next. `RunMonitor` answers every refusal as `ErrMonitoringOff`, so the monitor keeps the admit's own code and the triage fails with it.

**A write of the setting cancels every queued automatic triage whose run has not started**, the ones with no `triage_run_id`, which queueing clears, in the write's own transaction: each ends `failed` as `setting_changed`, its `triage_request_id` cleared, and its incident alerts in that transaction. Turning it off, changing the model, and turning it off and on again all cancel alike, so a triage runs only under the setting it was queued under, and none is moved to the new model. A worker that already took a cancelled request is refused at its admit and writes no outcome, as step 4 has it. A triage whose run has started finishes. A manual triage is left alone. The setting applies from then to occurrences that start after it.

**The cap is 6 automatic triages started per target per hour**, `maxAutomaticTriagesPerHour`, counted from `monitor_triage_starts`, a table of the monitor's own with a row per automatic start: its `target_id`, which references `monitor_targets(id)` with `ON DELETE CASCADE` so the rows go with their target, and its time, on the monitor's clock. `agent_runs` names a cluster and no target, and an incident keeps only its newest run. A triage the user starts by hand is not counted and not capped.

**An automatic triage decides its incident's alert, whatever the setting is by the time it ends.** The request carries its trigger, and one function of the setting, the incident and its triage decides each start (`alert.go`); the outcome alerts an automatic triage that failed or answered `ATTENTION`. With the setting off, an occurrence alerts as step 3b has it. An incident that has not alerted does so when an automatic triage of it:

- answers `ATTENTION`;
- fails, as a provider's error or a server that changed does;
- cannot start: the cap is reached, the setting changed, the model no longer resolves (`model_unavailable`), there is no sandbox, or the target was switched off;
- is not queued because a manual one is;
- is found stranded at a start, which step 4's sweep (`failStrandedTriages`) marks `failed` as `interrupted`.

`alerted_at` is written in the transaction that records the outcome: the verdict, the failure, the cancel or the sweep. The alert feed and the host are unchanged: the host shows an alert whose `alerted_at` is after its own launch. So an automatic triage that the app's stop cuts off writes no outcome then, which would raise no notification; it stays queued, and the next start's sweep fails it and alerts. An incident is never left with neither a report nor a notification, through a setting turned off or an app that exits mid-run.

A `DISMISS` raises nothing and sets no `alerted_at`. An incident still alerts at most once, which is step 3b's rule unchanged: an incident dismissed at first has not alerted, so its third occurrence's triage alerts it when it answers `ATTENTION` or fails. An incident that has alerted is still triaged at its third occurrence, for the report, and alerts no more. `incidents.baseline` and `alerted_at IS NULL` now differ: a dismissed incident is not `baseline` and has not alerted.

**GraphQL** adds `MonitoringTriageModel` (bound onto `monitor.TriageModel`), its input, the watch and the mutation, and `IncidentTriageError` gains `CapReached` and `SettingChanged`, each with its text in `TRIAGE_ERROR_TEXT`.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Setting | Planned | — |
| 2. Automatic queueing and the cap | Planned | — |
| 3. Alert rule | Planned | — |
| 4. Webview | Planned | — |

1. **Setting.** The `Entry`, its exported encoding, `KeySettings`, the watch, `monitoringTriageModelSet` with its cancel, and `incidentTriage`'s `always`.
2. **Automatic queueing and the cap.** The two triggers, `triage_trigger`, `monitor_triage_starts`, and the admit's cap.
3. **Alert rule.** The rule as one function of the setting, the incident and its triage, replacing step 3b's, and the outcome's, the cancel's and the sweep's `alerted_at`.
4. **Webview.** The checkbox and the tab's section, each folding the watch.

## Acceptance

- Deleting a target deletes its `monitor_triage_starts` rows.
- With the setting unset, no run starts without the user's press (`TestNoTriageStartsUnaskedWhileAutomaticTriageIsOff`).
- With it set, a new incident is triaged; `ATTENTION` alerts and `DISMISS` does not.
- A `baseline` occurrence is never triaged unasked, the third of its incident included.
- An incident whose first ordinary occurrence is its third is triaged once.
- An incident opened while the setting was on, and still queued when it is turned off, takes no run: its triage fails as `setting_changed` and it alerts untriaged.
- A worker paused before its insert, whose request a setting change cancels, starts no run when it resumes, though a manual request or a later occurrence has queued the incident again; the new request runs with its own model (`TestACancelledTriageStartsNoRun`).
- An automatic triage whose run was cut off by an app exit is marked `failed` at the next start and its incident alerts then, with the setting on or off.
- Ticking the checkbox and closing the picker writes no setting; ticking it and pressing *Triage with AI* writes it, and the tab shows it on.
- A ticked submission for an incident already queued is refused and leaves the setting unset.
- An incident triaged at its first occurrence and queued again at its third, with the setting then turned off before the run starts, has that request cancelled: its earlier run does not pass for a started one.
- Turning the setting off and on again revives no queued triage, and nor does setting another model (`TestAnAutomaticTriageRunsOnlyUnderItsSetting`).
- Unsetting it leaves a triage whose run has started to finish, and leaves a queued manual one queued.
- The seventh automatic triage of an hour on one target takes no run, and its incident alerts untriaged and says the cap was reached.
- A refused automatic triage adds no row to `monitor_triage_starts`, and a run inserted before a restart is counted after it.
- A triage the user starts takes no slot and is not refused at the cap.
- The tab's section states the cap.
- A model whose provider was signed out fails the triage and alerts.
- An incident dismissed at first and answered `ATTENTION` at its third occurrence alerts once; one dismissed at first whose third-occurrence triage fails alerts too.
- An incident that alerted at first raises nothing at its third occurrence, whatever that triage answers.
- A first ordinary occurrence that starts while the user's own triage of its incident is queued queues nothing, and the incident alerts.
- The setting turned on in one window shows as on in another, with no reload, and the watch sends its value at once.

## When it lands

- `apps/desktop/docs/persisted-data.md` gains `monitor_triage_starts`, `triage_trigger` and the `monitoring.triage.model` key.
- A security record, `apps/desktop/docs/security/<date>-automatic-triage.md`: monitor runs that start with nobody watching, the setting that allows them and is checked again when each run starts, the cap, and a model's verdict deciding whether the user is alerted.
- `apps/desktop/sidecar/internal/agent/CLAUDE.md`'s monitor paragraph names the automatic triage, the cap and the alert rule.
- `apps/desktop/src/components/settings/CLAUDE.md`'s **Monitoring** line gains the section.
