# Desktop monitoring

Kubetail Desktop watches each cluster for a short list of failures, records each as an incident, and alerts the user. A model can triage an incident: say what is happening, the likely cause and what to do next. Detection uses no model and is on by default. Triage spends model calls and runs only when the user asks, or after the user turns automatic triage on.

Answers #1606. The work is built on `wip/desktop-monitoring` and lands on `main` in one PR.

| Step | Spec | Needs | Status | PR |
| --- | --- | --- | --- | --- |
| 1 | [Detect](1-detect.md) | — | Planned | — |
| 2 | [Serve](2-serve.md) | 1 | Planned | — |
| 3a | [Incidents view](3a-incidents-view.md) | 2 | Planned | — |
| 3b | [Notify](3b-notify.md) | 2 | Planned | — |
| 3c | [Settings](3c-settings.md) | 2 | Planned | — |
| 4 | [Triage](4-triage.md) | 3a | Planned | — |
| 5 | [Automatic triage](5-automatic-triage.md) | 3b, 3c, 4 | Planned | — |

Steps 1 to 3 are a whole feature with no model in it. Steps 4 and 5 each add one thing.

## Decisions

- **Detection is on by default; triage is off by default.** Detection reads the cluster's local cache and costs nothing. `clusters.monitoring_enabled` defaults on and gates detection. No model call is made for monitoring unless the user pressed *Triage with AI* on an incident or turned automatic triage on.
- **Monitoring runs while the app runs.** Closing the last window leaves the app in the tray, and monitoring continues. A quit app watches nothing, and the Monitoring tab says so.
- **The signals are a fixed list of states**, read from the cache with the sidecar's own SQL:

  | Signal | True while | Counts after |
  | --- | --- | --- |
  | `CrashLoopBackOff` | a container waits with reason `CrashLoopBackOff` | 2 min |
  | `ImagePullBackOff` | a container waits with reason `ImagePullBackOff` or `ErrImagePull` | 2 min |
  | `PodPending` | a pod's phase is `Pending` and neither signal above holds | 5 min |
  | `NodeNotReady` | a node's `Ready` condition is not `True` | 2 min |
  | `ReplicasUnavailable` | a Deployment, StatefulSet or DaemonSet has fewer ready than desired replicas, and none of its pods holds a signal above | 5 min |

- **An incident is a cluster, a subject and a signal.** The subject of a pod's signal is its top controller, found by following controller owner references (Pod → ReplicaSet → Deployment); a pod with no controller is its own subject. A node is its own subject. A subject is named by group, kind, namespace and name, never by UID, so a recreated workload keeps its incident.
- **An incident holds occurrences.** An occurrence starts when the signal has held for its window and ends when it has been absent for 5 minutes, at the time it was last seen. A signal that returns within 24 hours of the last occurrence's end adds an occurrence to the same incident. After 24 quiet hours it opens a new incident. An incident keeps its newest 100 occurrences and its true first-seen time.
- **Only a trusted cache opens or ends anything.** While a cluster's cache is syncing, stale or unreachable, its incidents stay as they are.
- **The first reading of a cluster is its baseline.** Every signal already true opens an incident marked `baseline`, at once, with no alert and no automatic triage. Turning a cluster's monitoring off ends its open occurrences and drops its baseline, so turning it on again takes a new one. An app restart takes none.
- **An alert is an OS notification from the host**, with fixed text and the subject's name. No model output reaches the OS. On macOS the host asks for notification permission at first launch.
- **What alerts.** With automatic triage off: an incident's first occurrence, unless it is `baseline`. With it on: an incident whose triage answers `ATTENTION`, or whose triage could not run or failed. A later occurrence of the same incident never alerts on its own.
- **A triage is one monitor run** (`chat.RunMonitor`), in the session the [monitoring session record](../../../apps/desktop/docs/security/2026-10-05-the-monitoring-session.md) describes: read-only, no network, no prompts, no Secret data. Its report opens with a verdict line, `ATTENTION` or `DISMISS`. A report with neither reads as `ATTENTION`.
- **A dismissed incident stays listed**, with its report.
- **Automatic triage is one setting for every cluster: a model.** Set, each new incident that is not `baseline` is triaged, and again at its third occurrence. At most 6 automatic triages start per cluster per hour; an incident past the cap alerts untriaged.
- **One package owns the feature in the sidecar**, `internal/agent/monitor`. It reads `services/cluster` and calls `agent/chat`, so the layering puts it in `agent/`.

## Cut

Each waits for evidence from the MVP.

- `OOMKilled`. It is an event, not a state: a container killed once keeps `lastState.terminated.reason` for good, and the cache holds no time for it. A container killed repeatedly reaches `CrashLoopBackOff`, which is detected.
- Signals read from logs or Kubernetes Events.
- Grouping incidents with different subjects under one cause.
- Muting a subject, and acknowledging an incident.
- Turning a report's next steps into a chat draft.
- A model, a brief or an interval the user sets per cluster.
- A spending budget in dollars.
- Carrying one triage's report into the next.
- Sharing folder grants with the monitor.

## Open question

**Where the incidents view sits in the window.** Step 3a builds it as a third route beside Chat and Dashboard, scoped to the window's cluster. The design library has no screen for it yet.

## When it lands

The PR from `wip/desktop-monitoring` into `main`, beyond what each step lists:

- Deletes this folder and closes #1606.
- Replaces `apps/desktop/docs/TODO.md`'s *Build the monitoring agent over `chat.RunMonitor`* with one entry per item under *Cut* worth keeping.
- Writes the desktop ADRs:
  - detect without a model, triage on request;
  - an incident is a cluster, a subject and a signal, and holds occurrences;
  - the host raises an alert with fixed text;
  - `monitoring_enabled` gates detection and the triage setting gates unattended runs.
- Adds `apps/desktop/docs/security-model/monitoring.md`, indexed from `security-model.md`, with a row per protection steps 3b, 4 and 5 pin.
