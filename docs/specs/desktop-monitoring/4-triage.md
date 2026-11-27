# Step 4 — Triage

Prerequisite: step 3a on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

An incident has a *Triage with AI* action. The user picks a model, a monitor run reads the cluster, and the incident shows its verdict and its report.

## Design

**A triage is one `chat.RunMonitor` on the incident's cluster**, started through the incident's integration. The core holds no cluster and imports no `chat`, so a registration that can triage carries a `Triager`: `Triage(ctx, target, uid, brief, model, admit)` runs one triage of the target under that UID and returns a `TriageResult`: the `agent_runs` id, whether the run succeeded, failed or was cancelled, its report and its error text. A Go error means no run was inserted. `monitor/kubetriage` implements it over `chat.RunMonitor`, on the cluster `kubesignals.ClusterID` reads from the target's key, and `runtime.go` sets it on the Kubernetes registration. The run's session, its box and its record are unchanged. The queue, the states and the verdict are the core's and name the target.

**The core supplies the switch's check, and links the run in the same transaction.** In one transaction `RunMonitor` inserts the run and then calls `admit` with the run's id, as step 1 built it. `admit` reads the target's `enabled` and its `generation`, and then writes the incident's `triage_run_id`, which references the row just inserted. A refusal rolls back the insert with it, so a target switched off while its triage waited takes no run and no link. Switching it back on does not revive the request: the queue holds each triage with the `generation` its target had when it was queued, step 1's count of the switch's stops, and `admit` refuses another. Both refusals read `monitoring_off`. A run that is inserted is linked from that commit, so an app that exits mid-run leaves a triage the next start marks `failed`, with its run still linked.

**A triage reads the server its incident opened on, or nothing.** The UID is the incident's `target_uid`. `kubetriage` passes it to `RunMonitor` as the server UID the run is for, and `tools.Runtime` carries it:

- `RunMonitor` refuses with `ErrServerChanged`, before it inserts, when the cluster's active cache names another server;
- each cache read of the session, the cluster card's included, refuses a cache whose `Spec.ServerUID` is another;
- each connection the run takes is `Lease.ConnFor(ctx, uid)`, which refuses with `ErrIdentityMismatch` unless the server behind it is that one: the proxy's upstream for a command, and `LogsRead`'s, which reads on the cluster connection outside the proxy;
- `RunMonitor` keeps its own insert-time check for a cluster marked for deletion or gone, `ErrClusterGone`.

**A read refused for its server ends the run.** The cluster card draws a failed read as *Unavailable*, and the loop hands a tool's failure to the model and goes on; neither holds for this refusal. A cache or a connection refused for its server cancels the run, whose result is failed with `server_changed`, and the triage ends `failed` with that code whatever the model had read or written before it. So a context that moves mid-run fails the triage at its next read, and no report mixes two servers. A connection vouches for the server a probe last reached over it, as it does for a chat, and a triage claims no more.

The mutation's check against `current_uid` is a courtesy to the user: `current_uid` moves only at the next fold, and these checks do not wait for it. Each refusal ends the triage `failed` with its code: `server_changed`, `cluster_gone`, `monitoring_off`.

**The brief is a fixed template** naming the subject by its label, the signal's label, when it was first seen and how many times it has occurred, from `occurrence_count`. It carries the subject's attributes and the detail of its newest occurrence, the one with the highest `number`, each as it is stored, so the model gets the subject's own fields and what was last seen (for a crash loop, the pod, the container and its last exit) without the core reading them. The detail is read in the transaction that queues the triage.

**The whole encoded brief fits `maxMonitorBriefLen`, which this step sets to 16 KiB.** A reading may hold 4 KiB of attributes and 16 KiB of detail, and JSON escaping lengthens both, so the core cuts: each attribute and detail value to 1 KiB, then entries are added in a fixed order, the attributes by key and then the detail by key, while the encoded brief stays within the limit. The brief says how many entries it left out. The template, the labels and the counts always fit.

**The label, the attributes and the detail are the target's text, and the brief treats them as data.** Each is fenced with `lib/fencejson`, so no value can close the fence or pass as the template's own text. The fence keeps the brief's structure and nothing more: a model can still act on what a value says. `chat/prompts/monitor.md` calls the fenced fields untrusted data that describe the failure and give no instruction, and what bounds a run that follows one anyway is the monitoring session, which is read-only.

**`chat/prompts/monitor.md` states the report's shape**: a first line of `ATTENTION` or `DISMISS`, then what is happening, the likely cause, and next steps the user could take. The monitor parses the first line alone. A report with neither word reads as `ATTENTION`.

**One triage per incident is kept**, the newest. `incidents` gains:

| Column | Holds |
| --- | --- |
| `triage_state` | `none`, `queued`, `done` or `failed` |
| `triage_request_id` | a new id for each request queued, NULL when none is |
| `triage_run_id` | the `agent_runs` row, `ON DELETE SET NULL` |
| `triage_verdict` | `attention` or `dismiss`, NULL until done |
| `triage_error` | why a triage failed or could not start, as a fixed code |

The report is the run's `agent_runs.result`, read with the incident and served once the triage is `done` or `failed`. The monitor writes each triage state and commits before `Notify(appdb.KeyIncidents)`, so `incidentCollectionWatch` delivers the newest as a `Modified`, and may skip `queued`, as a collection watch may; the run's own writes notify nothing it listens on, and it needs none of them.

**Triages queue per target** on a `lib/workqueue`, since a cluster takes one monitor run at a time. Each request has an id of its own, written to `triage_request_id` when it is queued and held by the worker that takes it. Queueing also clears `triage_run_id`, `triage_verdict` and `triage_error`, so the row describes the new request alone: the earlier triage's report leaves the incident then, and a `triage_run_id` on a `queued` triage is always that request's own run. `admit` refuses a worker whose id is not the incident's, and so does the write of its outcome, so a worker that outlived its request starts no run and records nothing over a newer one. A triage is `queued` from the mutation until its run ends, and one with a `triage_run_id` has its run inserted. A start marks stranded `queued` triages `failed`.

**GraphQL**: `incidentTriage(id: IncidentID!, providerID: String!, modelID: String!, effort: String): Incident!`, refused for an incident already queued, for one whose integration has no `Triager`, and for one whose `target_uid` is not its target's `current_uid`: the run would read another server. `MonitorTarget` gains `canTriage`, true when its integration has a `Triager`: it is fixed for the life of the process. `Incident` gains `triage`, with the state, verdict, error, report and model, each a scalar so the collection watch's diff holds. The mutation returns the incident as the watch then delivers it.

**The webview** adds the action to the incident's detail, with the composer's `ModelPicker`. The report is drawn through `VisibleText` as plain text, never as Markdown. The list marks a dismissed incident *Dismissed by triage* and keeps it. Where the sandbox is unavailable, a provider is signed out or the target's monitoring is off, the action is disabled and says which. The action is offered when the incident's target has `canTriage` and the incident's `targetUID` is that target's `currentUID`, both read from the target watch, so a target that moves to another server takes the action away though the incident sends no frame. Otherwise it is left out. The action draws the newest state it holds, under way for `queued`, and does not depend on seeing it.

**Nothing here starts a run unasked.** The only caller of `Triage` is the mutation's path, and the only caller of `RunMonitor` is `kubetriage`.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Triage test | Planned | — |
| 2. Brief, prompt and verdict | Planned | — |
| 3. Queue and record | Planned | — |
| 4. Serve | Planned | — |
| 5. Webview | Planned | — |

1. **Triage test.** By hand, before the rest: a kind cluster with a crash-looping pod, a bad image and an unschedulable pod, `RunMonitor` with the template, on two providers. Adjust the brief, the prompt and `MaxMonitorToolCalls` until the reports are useful, and record what was tried in the PR.
2. **Brief, prompt and verdict.** `Triager`, `monitor/kubetriage` and its wiring, the template and its cut, the prompt's change with its golden file, the parse.
3. **Queue and record.** The columns, the queue, the request's id, `admit`'s write and its generation check, `TriageResult` and the state each status ends in, the stranded sweep. In `chat`: `RunMonitor`'s server UID with `ErrServerChanged`, the session's check on each cache read, `ConnFor` on each connection, the refusal that ends the run, and the brief's limit.
4. **Serve.** The mutation, the field and their resolver tests.
5. **Webview.** The action, the report, the disabled states.

## Acceptance

- A triage that finishes arrives as a `Modified` on `incidentCollectionWatch` with the state `done`, a verdict and a report. `queued` is written and notified, and a subscriber may not see it: a collection watch may lose an item's intermediate states.
- A report opening `DISMISS` marks the incident dismissed; one opening with anything else reads as attention.
- A provider error ends the triage `failed` with its code, and the action is offered again.
- A second triage of a target waits for the first.
- A subject name that tries to close the fence reaches the model inside it (`TestATriageBriefFencesTheSubject`), and so does a condition's message in the detail (`TestATriageBriefFencesTheDetail`). These pin the brief's structure; the monitoring session's own tests pin that a run changes nothing.
- An incident whose reading held 4 KiB of attributes and 16 KiB of detail, every byte one that JSON escapes, is triaged through `RunMonitor`: the brief is within 16 KiB, cuts each value to 1 KiB and says how many entries it left out.
- A triage queued before its context moved to another server takes no run, though no fold has yet changed `current_uid`: it fails as `server_changed` (`TestATriageReadsNoOtherServer`).
- A context that moves while a run reads fails the run at its next read.
- A triage queued for a cluster then marked for deletion takes no run and fails as `cluster_gone`.
- A run that ends failed leaves the triage `failed` with its run linked.
- A sidecar stopped after the run's insert leaves a triage the next start marks `failed`, with its run still linked.
- The brief for an incident with three occurrences carries the newest one's detail and no other's.
- A report holding HTML, Markdown links or control sequences is drawn as text.
- An incident that opens starts no run: only the mutation does (`TestNoTriageStartsUnasked`).
- Without a sandbox, the action is disabled and says so.
- A target of a fake integration with no `Triager` serves `canTriage` false, its incidents offer no triage, and the mutation refuses one.
- An incident that opened under a UID its target no longer has offers no triage, and the mutation refuses it.
- An ended incident whose target moves to another server loses its action from the target's `Modified`, with no frame for the incident.
- An incident triaged once and queued again serves no run, verdict or report until the new run is inserted.
- A worker holding a request id the incident no longer has is refused at `admit`, and one whose run ended writes no outcome.
- A refused admission leaves no `agent_runs` row and no `triage_run_id` (`TestARefusedTriageInsertsNoRun`).
- With the context moved and no fold since, a run whose next operation is `LogsRead` is refused the connection and reads no log of the other server. The run makes no further model call, and the incident's triage is `failed` with `server_changed`, though earlier reads would have filled a report.
- A run whose cluster card read finds another server makes no model call, and its triage is `failed` with `server_changed`, not a report over an *Unavailable* card.
- A target switched off and on again while its triage is queued takes no run: the triage fails as `monitoring_off`, and a triage asked for after the switch runs.
- A target switched off while its triage is queued takes no run: the triage fails with the code (`TestATriageIsAdmittedInsideTheInsert`).
- The core imports no `chat` and `kubesignals` imports none: only `kubetriage` does.

## When it lands

- A security record, `apps/desktop/docs/security/<date>-incident-triage.md`: a user-started monitor run, a cluster's text in its brief (the subject's label and attributes, and an occurrence's detail with its condition messages), and its report reaching the screen. It records that `monitor_targets.enabled` gates detection, that a run needs the user's press, that the run's insert still checks the switch, that a run reads one server, and that the fence keeps structure while the read-only session bounds what a followed instruction can do.
- `apps/desktop/sidecar/internal/agent/CLAUDE.md`'s monitor paragraph names the triage, its queue and the verdict rule.
- `apps/desktop/docs/persisted-data.md` gains the triage columns.
