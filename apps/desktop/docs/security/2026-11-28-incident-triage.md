# Security record — Incident triage, 28 November 2026

**Subject:** an incident's *Triage with AI* action. The user picks a model; the monitor queues
the triage per target (`internal/agent/monitor/triage.go`) and runs it through the Kubernetes
integration's `Triager`, `monitor/kubetriage`, as one `chat.RunMonitor` on the incident's cluster.
The run's brief carries the incident's cluster text, and its report reaches the incident's detail.
The living model is [security-model.md](../security-model.md); the session is
[the monitoring session](2026-10-05-the-monitoring-session.md).

## What widens

**A monitor run now has a caller.** The user's press queues one; nothing else does. The run reads
the cluster unattended, in the monitoring session, and spends model calls.

**A cluster's text reaches the model as a brief.** The subject's label and attributes, and the
detail of the incident's newest occurrence (a pod, a container, a condition's message), are text a
cluster wrote. They ride the run's first message.

**Model output reaches the screen.** The run's report, written by a model that read the cluster, is
drawn in the incident's detail, and its first line sets the incident's verdict.

## What holds it

- **Only the user's press starts a run.** The mutation `incidentTriage` is the one path to
  `Triager.Triage`, and `kubetriage` the one caller of `RunMonitor`; an incident that opens starts
  none (`TestNoTriageStartsUnasked`).
- **`monitor_targets.enabled` still gates the run.** The monitor's admit runs inside the transaction
  that inserts the run, after the insert (`TestRunMonitorAdmitsInsideTheInsert`), and links the run
  to its triage only while the target is on under the generation the triage was queued with. A
  target switched off while its triage waited, or off and on again, takes no run and no link
  (`TestATriageIsAdmittedInsideTheInsert`, `TestATriageQueuedBeforeASwitchIsNotRevived`), and a
  worker whose request was replaced is refused and records nothing
  (`TestAWorkerThatOutlivedItsRequestWritesNothing`).
- **A run reads one server.** It is for the UID the incident opened under. `RunMonitor` refuses
  before its insert once the cluster's card names another server (`TestRunMonitorRefuses`,
  `TestATriageReadsNoOtherServer`); during the run, the card, `KubeQuery`, `LogsRead` and the log
  service refuse a cache of another server, and a command's claim and the log service refuse a
  connection that reaches one (`TestCardAtAServerIsRefusedForAnother`,
  `TestACacheOfAnotherServerIsRefusedAndEndsTheRun`, `TestAReadOfAnotherServerIsRefusedAndEndsTheRun`,
  `TestAFetchForOneServerReadsNoOther`, `TestATargetForAnotherServerIsRefused`,
  `TestAConnectionToAnotherServerTellsTheRun`). Any of these ends the run failed with
  `server_changed`, with no further model call (`TestAReadRefusedForItsServerEndsTheRun`).
- **The brief keeps its structure.** Every cluster string is one JSON string inside one fenced
  block, through `lib/fencejson`, so no value closes the fence or passes as the template's text
  (`TestATriageBriefFencesTheSubject`, `TestATriageBriefFencesTheDetail`). The brief fits its cap,
  each value cut and the entries left out counted (`TestATriageBriefAtTheBoundsFitsTheLimit`). The
  prompt calls the fenced fields untrusted data that give no instruction.
- **What a followed instruction can do is the session's.** The run is read-only at the proxy, never
  reads Secret data, asks nobody, has no network and no folder grant, as the monitoring session
  record holds; nothing here widens it.
- **The report is drawn as text.** The detail draws it through `VisibleText`, never as Markdown and
  with no HTML sink, so markup, links and control sequences show as characters (`draws a done
  triage’s verdict, model and report as plain text`). Its verdict is a fixed word: a first line that is the word
  `DISMISS` dismisses, and anything else reads as attention (`TestTheVerdictIsTheReportsFirstLine`).

## What narrows

Nothing for a chat: a run with no server UID reads as before.

## Residuals

- The fence keeps the brief's structure and nothing more: a model can still act on what a value
  says, inside the read-only session.
- A dismissed incident stays listed: a model that was misled hides nothing, but the verdict is the
  model's word.
- A connection vouches for the server a probe last reached over it, as it does for a chat.
