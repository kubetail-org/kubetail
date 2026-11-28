# Monitoring

[← Security model](../security-model.md)

The monitor reads each cluster's local cache with no model, records what it finds as incidents, alerts the user through the OS, and can triage an incident with a monitor run. The rule is that model spending starts only on the user's press or the user's setting, and that a run reads one server inside the read-only monitoring session ([the monitoring session](../security/2026-10-05-the-monitoring-session.md)). Everything a cluster returns is attacker-controlled text: it reaches a notification only as a bounded label, a brief only as fenced data, and the screen only as text. Detection is on for every cluster by default, by decision.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| MON-1 | With automatic triage off, no monitor run starts without the user's press. | **Enforced** | — |
| MON-2 | Automatic triage is off by default, and a stored value Kubetail cannot use reads as off. Only a press with the box ticked turns it on. | **Enforced** | — |
| MON-3 | A target's switch gates every run, checked inside the run's insert under the generation the triage was queued with. | **Enforced** | — |
| MON-4 | An automatic triage runs only under the setting it was queued for; a write of the setting fails every one not yet started. | **Enforced** | — |
| MON-5 | At most 6 automatic triages start per target per hour, counted across restarts. | **Enforced** | The cap is per target, so many targets can each spend 6 an hour. |
| MON-6 | A baseline occurrence is never triaged unasked. | **Enforced** | — |
| MON-7 | An automatic triage that fails, cannot start, is cancelled or is stranded alerts its incident. | **Enforced** | — |
| MON-8 | A triage reads only the server its incident opened on, and ends `server_changed` on reaching another. | **Enforced** | A connection vouches for the server a probe last reached over it, as for a chat. |
| MON-9 | Every cluster string in a triage's brief is a JSON string inside one fenced block, within the brief's cap. | **Enforced** | The fence keeps the structure alone: a model can still act on what a value says, inside the read-only session. |
| MON-10 | A triage's report is drawn as text, and its verdict is a fixed word: only a first line of `DISMISS` dismisses. | **Enforced** | — |
| MON-11 | Each reading is bounded before it is stored: a label, the attributes and the detail over their bounds make the signal `invalid`. | **Enforced** | — |
| MON-12 | No page can send an OS notification. | **Enforced** | — |
| MON-13 | A notification's body is fixed text around the labels, each stripped of control and format characters, bounded, and escaped on Linux. | **Enforced** | — |
| MON-14 | A notification's press reaches only the page of the window it surfaced, as an incident's id. | **Enforced** | — |
| MON-15 | No model output reaches an OS notification. | **Held by review** | — |
| MON-16 | macOS asks for notification permission once, and shows and reports a press through its notification center. | **Built** | Checked by hand in a signed build: the center serves no unsigned app. |
| MON-17 | Every cluster's target is monitored by default. | **By decision** | — |
| MON-18 | A label within its bounds is shown as the cluster wrote it. | **By decision** | A cluster can name a workload so its notification reads as another's. |
| MON-19 | The OS keeps the notifications it showed in its history. | **By decision** | They outlive the incident's 30-day retention. |
| MON-20 | An automatic triage that answers `DISMISS` raises no notification. | **By decision** | A model misled by a cluster's text can silence a real incident's notification; the incident stays listed with its report. |

## Evidence

### MON-1

- **Where:** `sidecar/internal/agent/monitor`, `sidecar/internal/agent/monitor/kubetriage`
- **Records:** [incident triage](../security/2026-11-28-incident-triage.md), [automatic triage](../security/2026-11-28-automatic-triage.md)
- `TestNoTriageStartsUnaskedWhileAutomaticTriageIsOff` — an incident that opens with the setting unset starts no run.
- Note: `incidentTriage` is the one path to `Triager.Triage`, and `kubetriage` the one caller of `RunMonitor`.

### MON-2

- **Where:** `sidecar/internal/services/settings`, `sidecar/internal/agent/monitor`
- **Records:** [automatic triage](../security/2026-11-28-automatic-triage.md)
- `TestAMonitoringTriageModelKubetailCannotUseIsUnset` — no row, a NULL, or a value Kubetail cannot use reads as unset.
- `TestATickedTriageWritesTheSettingWithIt` — the setting is written in the triage's own transaction, and a refused triage writes nothing.

### MON-3

- **Where:** `sidecar/internal/agent/chat`, `sidecar/internal/agent/monitor`
- **Records:** [monitoring on by default](../security/2026-11-27-monitoring-on-by-default.md), [incident triage](../security/2026-11-28-incident-triage.md)
- `TestRunMonitorAdmitsInsideTheInsert`, `TestRunMonitorRefuses` — `RunMonitor` calls its admit inside the run's insert, and a refusal writes nothing.
- `TestATriageIsAdmittedInsideTheInsert`, `TestATriageQueuedBeforeASwitchIsNotRevived` — a target switched off, or off and on again, while its triage waited takes no run.
- `TestAWorkerThatOutlivedItsRequestWritesNothing` — a worker whose request was replaced records nothing.

### MON-4

- **Where:** `sidecar/internal/agent/monitor`
- **Records:** [automatic triage](../security/2026-11-28-automatic-triage.md)
- `TestAnAutomaticTriageRunsOnlyUnderItsSetting`, `TestASettingWriteCancelsTheQueuedAutomaticTriages` — a changed or cleared setting fails the queued automatic triages as `setting_changed`.
- `TestACancelledTriageStartsNoRun` — a worker that already took a cancelled request is refused at its admit.

### MON-5

- **Where:** `sidecar/internal/agent/monitor`
- **Records:** [automatic triage](../security/2026-11-28-automatic-triage.md)
- `TestTheSeventhAutomaticTriageOfAnHourTakesNoRun` — the seventh of an hour takes no run and no slot, and its incident alerts untriaged.
- Note: `monitor_triage_starts` holds the count, so a restart does not reset it. The user's own press is not capped.

### MON-6

- **Where:** `sidecar/internal/agent/monitor`
- **Records:** [automatic triage](../security/2026-11-28-automatic-triage.md)
- `TestAnOccurrenceAlertsOrQueuesByTheRule`, `TestTheThirdOccurrenceQueuesATriageUnlessItIsBaseline` — a baseline occurrence queues no automatic triage.

### MON-7

- **Where:** `sidecar/internal/agent/monitor`
- **Records:** [automatic triage](../security/2026-11-28-automatic-triage.md)
- `TestATriagesOutcomeAlertsByTheRule`, `TestAnAutomaticTriageThatCannotStartAlerts`, `TestAStrandedTriageIsFailedAtStart` — the outcome alerts in the transaction that records it.
- `TestAnAutomaticTriageCutOffByTheStopAlertsAtTheNextStart` — one the stop cuts off alerts at the next start's sweep.
- `TestAnOccurrenceUnderAQueuedTriageQueuesNothing` — a first ordinary occurrence under the user's queued triage alerts at once.

### MON-8

- **Where:** `sidecar/internal/agent/chat`, `sidecar/internal/agent/monitor/kubetriage`, `sidecar/internal/tools`, `sidecar/internal/services/logs`
- **Records:** [incident triage](../security/2026-11-28-incident-triage.md)
- `TestATriageReadsNoOtherServer` — `RunMonitor` refuses before its insert once the card names another server.
- `TestCardAtAServerIsRefusedForAnother`, `TestACacheOfAnotherServerIsRefusedAndEndsTheRun`, `TestAReadOfAnotherServerIsRefusedAndEndsTheRun`, `TestAFetchForOneServerReadsNoOther` — the card, `KubeQuery`, `LogsRead` and the log service refuse another server's cache.
- `TestATargetForAnotherServerIsRefused`, `TestAConnectionToAnotherServerTellsTheRun` — a command's claim refuses a connection that reaches another server.
- `TestAReadRefusedForItsServerEndsTheRun` — any of these ends the run with no further model call.

### MON-9

- **Where:** `sidecar/internal/agent/monitor`
- **Records:** [incident triage](../security/2026-11-28-incident-triage.md)
- `TestATriageBriefFencesTheSubject`, `TestATriageBriefFencesTheDetail` — no value closes the fence or passes as the template's text.
- `TestATriageBriefAtTheBoundsFitsTheLimit` — each value is cut and the entries left out are counted.

### MON-10

- **Where:** `sidecar/internal/agent/monitor`, `src/components/monitoring/incidents-dialog`
- **Records:** [incident triage](../security/2026-11-28-incident-triage.md)
- `TestTheVerdictIsTheReportsFirstLine` — a first line of `DISMISS` dismisses, and anything else reads as attention.
- Note: the detail draws the report through `VisibleText`, never as Markdown (`triage.test.tsx`).

### MON-11

- **Where:** `sidecar/internal/agent/monitor`, `sidecar/internal/agent/monitor/kubesignals`
- **Records:** [monitoring on by default](../security/2026-11-27-monitoring-on-by-default.md)
- `TestAnObservationOverABoundMakesItsSignalInvalid` — the core's check drops an observation over a bound.
- `TestALongOrControlNameIsCutToFit` — the Kubernetes integration cuts what it sends to fit.
- `TestEachKindReadSearchesItsIndex` — every statement names its tables `main.`.

### MON-12

- **Where:** `src-tauri/src/window_manager.rs`, `src-tauri/capabilities`
- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md)
- `no_window_may_send_a_notification`, `default_capability_grants_what_the_app_window_calls` — no capability grants a `notification:` permission.

### MON-13

- **Where:** `src-tauri/src/alerts`
- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md)
- `alert_body_strips_control_characters_and_bounds_each_part` — 120 characters for the subject, 60 each for the target and the signal.
- `a_linux_body_escapes_markup_in_labels` — `&`, `<` and `>` are escaped whatever the server says it reads.

### MON-14

- **Where:** `src-tauri/src/commands.rs`
- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md)
- `notification_press_take_takes_the_callers_press` — the press is read off the caller's `Webview`.

### MON-15

- **Where:** `sidecar/internal/agent/monitor`, `src-tauri/src/alerts`
- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md), [automatic triage](../security/2026-11-28-automatic-triage.md)
- Note: an alert carries the labels the integration wrote and no field of the triage. → [ADR: raise an alert from the host, with fixed text](../adr/2026-11-28-the-host-raises-an-alert-with-fixed-text.md)

### MON-16

- **Where:** `src-tauri/src/alerts/macos.rs`, `src-tauri/src/alerts/permission.rs`
- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md)
- Note: a host outside a bundle, as `tauri dev` runs it, never calls the center and raises no notification.

### MON-17

- **Records:** [monitoring on by default](../security/2026-11-27-monitoring-on-by-default.md)
- Note: detection reads the local cache and spends nothing; the switch still gates every run (MON-3). → [ADR: gate detection with the target's switch](../adr/2026-11-28-the-target-switch-gates-detection-and-the-triage-setting-gates-unattended-runs.md)

### MON-18

- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md)
- Note: the subject's kind and namespace lead its label.

### MON-19

- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md)

### MON-20

- **Records:** [automatic triage](../security/2026-11-28-automatic-triage.md)
- Note: anything but `DISMISS` alerts, and so does a triage that could not run (MON-7).
