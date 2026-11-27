# Step 5 — Automatic triage

Prerequisites: steps 3b, 3c and 4 on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

The user can leave triage on. Each new incident is then triaged unasked, and only one that needs attention alerts.

## Design

**One setting, `monitoring.triage.target`**, a row of `settings` for every cluster: a provider, a model and an effort as JSON, or unset. Set means automatic triage is on. Its strictest value is unset.

**The user sets it in two places**: a checkbox beside *Triage with AI*, *Always triage new incidents with this model*, and a section of the Monitoring tab with the model and a way to turn it off. The tab states that this spends model calls while nobody is watching, and shows today's triage cost across clusters, from `monitoringTriageCostStateWatch`.

**With the setting on**, the monitor queues a triage when:

- an incident starts its first occurrence that is not `baseline`;
- an incident reaches its third occurrence, in the transaction that takes its `occurrence_count` to 3, with the brief saying so.

**The cap is 6 automatic triages started per cluster per hour**, counted from `agent_runs`. A triage the user starts by hand is not counted and not capped.

**The alert rule changes with the setting.** On, an incident alerts when its triage answers `ATTENTION`, when its triage fails, or when no triage can start: the cap is reached, the model no longer resolves, or there is no sandbox. A `DISMISS` raises nothing. An incident still alerts at most once, except that a third-occurrence triage answering `ATTENTION` alerts an incident whose first triage dismissed it.

**A change of the setting applies to incidents that open after it.**

**Two reads**, each from [`apps/desktop/docs/read-contracts/`](../../../apps/desktop/docs/read-contracts/README.md):

| Read | Contract | Go | GraphQL |
| --- | --- | --- | --- |
| The setting | Get | `GetMonitoringTriageTarget` | `monitoringTriageTargetGet` |
| Today's triage cost | State watch | `WatchMonitoringTriageCostState` | `monitoringTriageCostStateWatch` |

- **The setting is a Get**, as `permissionSettingsGet` is: only the user's writes change it. `monitoringTriageTargetSet` returns what `monitoringTriageTargetGet` then answers, so the caller needs no second read.
- **Today's cost changes as triages run, unasked, so it is a state watch.** Its value is the cost in micro-dollars of every monitor run that started since local midnight, every cluster's, and the day it counts. The first frame is sent at once. It listens on `KeyIncidents` and `KeyMonitorRuns` before it reads, re-reads on each notice and at local midnight, and sends the whole value each time. A monitor run notifies `KeyMonitorRuns` after the commit that records its cost, since a `DISMISS` may change no incident.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Setting | Planned | — |
| 2. Automatic queueing and the cap | Planned | — |
| 3. Alert rule | Planned | — |
| 4. Webview | Planned | — |

1. **Setting.** The `Entry`, its typed methods, `monitoringTriageTargetGet` and `monitoringTriageTargetSet`.
2. **Automatic queueing and the cap.** The two triggers and the hourly count.
3. **Alert rule.** The rule as one function of the setting, the incident and its triage, replacing step 3b's.
4. **Webview.** The checkbox, the tab's section, and today's cost folded with `stateWatchReducer`.

## Acceptance

- With the setting unset, no run starts without the user's press (`TestNoTriageStartsUnaskedWhileAutomaticTriageIsOff`).
- With it set, a new incident is triaged; `ATTENTION` alerts and `DISMISS` does not.
- A `baseline` occurrence is never triaged unasked.
- The seventh incident of an hour on one cluster alerts untriaged and says the cap was reached.
- A model whose provider was signed out fails the triage and alerts.
- An incident dismissed at first and answered `ATTENTION` at its third occurrence alerts once.
- Unsetting it stops new triages and leaves a running one to finish.
- `monitoringTriageCostStateWatch` sends its value at once, again when a triage ends, and again at local midnight with the new day's.

## When it lands

- A security record, `apps/desktop/docs/security/<date>-automatic-triage.md`: monitor runs that start with nobody watching, the setting that allows them, the cap, and a model's verdict deciding whether the user is alerted.
- `apps/desktop/src/components/settings/CLAUDE.md`'s **Monitoring** line gains the section.
