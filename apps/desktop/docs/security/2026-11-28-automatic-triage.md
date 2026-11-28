# Security record — Automatic triage, 28 November 2026

**Subject:** the `monitoring.triage.model` setting. Set, each new incident that is not baseline is
triaged unasked, and again at its third occurrence, by the monitor run [incident
triage](2026-11-28-incident-triage.md) describes; only an incident that needs attention alerts.
The setting is an entry of `services/settings` (`monitoring.go`), which the monitor reads and writes
on its own transactions (`internal/agent/monitor/setting.go`); the rule is `monitor/alert.go`. The
living model is [security-model.md](../security-model.md); the session is [the monitoring
session](2026-10-05-the-monitoring-session.md).

## What widens

**A monitor run starts with nobody watching.** With the setting on, an occurrence's start queues a
run that reads the cluster and spends model calls while the user may be away.

**A model's verdict decides whether the user is alerted.** An automatic triage that answers
`DISMISS` raises no notification; the incident stays in the bell, marked dismissed.

## What holds it

- **Off is the default and the strictest value.** No row, a NULL, or a stored value Kubetail cannot
  use reads as unset (`TestAMonitoringTriageModelKubetailCannotUseIsUnset`), and with it unset no
  run starts without the user's press (`TestNoTriageStartsUnaskedWhileAutomaticTriageIsOff`). The
  user turns it on only with a press of *Triage with AI* with the box ticked, which writes it in the
  triage's own transaction; a refused triage writes nothing (`TestATickedTriageWritesTheSettingWithIt`).
- **The setting is checked again when each run starts.** A write of the setting fails every
  automatic triage whose run has not started, as `setting_changed`, so none runs under a model the
  user since changed or turned off (`TestAnAutomaticTriageRunsOnlyUnderItsSetting`,
  `TestASettingWriteCancelsTheQueuedAutomaticTriages`), and a worker that already took a cancelled
  request is refused at its admit and starts no run (`TestACancelledTriageStartsNoRun`). The target's
  switch and generation are checked there too, as for a manual triage.
- **The cap bounds the spend.** At most 6 automatic triages start per target per hour, counted in
  the admit's transaction from `monitor_triage_starts`, which outlives a restart; a refused triage
  takes no slot, and its incident alerts untriaged (`TestTheSeventhAutomaticTriageOfAnHourTakesNoRun`).
  The user's own press is not capped.
- **No incident is left with neither a report nor a notification.** An automatic triage that fails,
  cannot start, is cancelled or is found stranded at a start alerts its incident in the transaction
  that records it (`TestATriagesOutcomeAlertsByTheRule`, `TestAnAutomaticTriageThatCannotStartAlerts`,
  `TestAStrandedTriageIsFailedAtStart`); one the stop cuts off is left for the next start's sweep,
  which alerts after the host has launched (`TestAnAutomaticTriageCutOffByTheStopAlertsAtTheNextStart`).
  A first ordinary occurrence under the user's own queued triage alerts at once
  (`TestAnOccurrenceUnderAQueuedTriageQueuesNothing`).
- **A baseline occurrence is never triaged unasked** (`TestAnOccurrenceAlertsOrQueuesByTheRule`,
  `TestTheThirdOccurrenceQueuesATriageUnlessItIsBaseline`).
- **The verdict is a fixed word, and the alert is fixed text.** The first line's `DISMISS` is the one
  answer that silences; anything else alerts. No model output reaches the OS notification.

## What narrows

Nothing: a manual triage, the session and the brief are unchanged.

## Residuals

- A model misled by a cluster's text can answer `DISMISS` and silence a real incident's
  notification. The incident stays listed with its report.
- The cap is per target: many targets can spend 6 triages each an hour.
- A queued automatic triage runs with the model the setting named when it was queued; a provider
  signed out since fails it, and the incident alerts.
