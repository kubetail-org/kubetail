# Step 4 — Triage

Prerequisite: step 3a on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

An incident has a *Triage with AI* action. The user picks a model, a monitor run reads the cluster, and the incident shows its verdict, its report and what the run cost.

## Design

**A triage is one `chat.RunMonitor` on the incident's cluster**, started by the monitor package. Its session, its box and its record are unchanged.

**The brief is a fixed template** naming the subject, the signal, when it was first seen and how many times it has occurred, from `occurrence_count`. The subject's names are cluster text: each is bounded in length and fenced with `lib/fencejson`, so a name cannot read as an instruction.

**`chat/prompts/monitor.md` states the report's shape**: a first line of `ATTENTION` or `DISMISS`, then what is happening, the likely cause, and next steps the user could take. The monitor parses the first line alone. A report with neither word reads as `ATTENTION`.

**One triage per incident is kept**, the newest. `incidents` gains:

| Column | Holds |
| --- | --- |
| `triage_state` | `none`, `queued`, `running`, `done` or `failed` |
| `triage_run_id` | the `agent_runs` row, `ON DELETE SET NULL` |
| `triage_verdict` | `attention` or `dismiss`, NULL until done |
| `triage_error` | why a triage failed or could not start, as a fixed code |

The report is the run's `agent_runs.result` and the cost is the sum of its `llm_calls.cost_usd_micros`, both read with the incident, and served once the triage is `done` or `failed`. The monitor writes each triage state and commits before `Notify(appdb.KeyIncidents)`, so `incidentCollectionWatch` delivers every state as a `Modified`; the run's own writes notify nothing it listens on, and it needs none of them.

**Triages queue per cluster** on a `lib/workqueue`, since a cluster takes one monitor run at a time. A start marks stranded `queued` and `running` triages `failed`.

**GraphQL**: `incidentTriage(id: IncidentID!, providerID: String!, modelID: String!, effort: String): Incident!`, refused for an incident already queued or running. `Incident` gains `triage`, with the state, verdict, error, report, cost and model, each a scalar so the collection watch's diff holds. The mutation returns the incident as the watch then delivers it.

**The webview** adds the action to the incident's detail, with the composer's `ModelPicker`. The report is drawn through `VisibleText` as plain text, never as Markdown. The list marks a dismissed incident *Dismissed by triage* and keeps it. Where the sandbox is unavailable, a provider is signed out or the cluster's monitoring is off, the action is disabled and says which.

**Nothing here starts a run unasked.** The only caller of `RunMonitor` is the mutation's path.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Triage test | Planned | — |
| 2. Brief, prompt and verdict | Planned | — |
| 3. Queue and record | Planned | — |
| 4. Serve | Planned | — |
| 5. Webview | Planned | — |

1. **Triage test.** By hand, before the rest: a kind cluster with a crash-looping pod, a bad image and an unschedulable pod, `RunMonitor` with the template, on two providers. Adjust the brief, the prompt and `MaxMonitorToolCalls` until the reports are useful, and record what was tried in the PR.
2. **Brief, prompt and verdict.** The template, the prompt's change with its golden file, the parse.
3. **Queue and record.** The columns, the queue, the stranded sweep.
4. **Serve.** The mutation, the field and their resolver tests.
5. **Webview.** The action, the report, the disabled states.

## Acceptance

- Triaging an incident moves it through `queued` and `running` to `done`, each state one `Modified` on `incidentCollectionWatch`, the last with a verdict, a report and a cost.
- A report opening `DISMISS` marks the incident dismissed; one opening with anything else reads as attention.
- A provider error ends the triage `failed` with its code, and the action is offered again.
- A second triage of a cluster waits for the first.
- A subject name that reads as an instruction reaches the model inside the fence (`TestATriageBriefFencesTheSubjectsNames`).
- A report holding HTML, Markdown links or control sequences is drawn as text.
- An incident that opens starts no run: only the mutation does (`TestNoTriageStartsUnasked`).
- Without a sandbox, the action is disabled and says so.

## When it lands

- A security record, `apps/desktop/docs/security/<date>-incident-triage.md`: a user-started monitor run, cluster text in its brief, and its report reaching the screen. It records that `monitoring_enabled` gates detection and that a run needs the user's press.
- `apps/desktop/sidecar/internal/agent/CLAUDE.md`'s monitor paragraph names the triage, its queue and the verdict rule.
- `apps/desktop/docs/persisted-data.md` gains the triage columns.
