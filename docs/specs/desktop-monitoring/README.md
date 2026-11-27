# Desktop monitoring

Kubetail Desktop watches each cluster for a short list of failures, records each as an incident, and alerts the user. A model can triage an incident: say what is happening, the likely cause and what to do next. Detection uses no model and is on by default. Triage spends model calls and runs only when the user asks, or after the user turns automatic triage on.

Kubernetes is the one integration this work builds. The monitor is shaped so that another (an AWS account, a GCP project, an Azure subscription, a Cloudflare account, a host) is a new package behind one interface and nothing else moves: [*Another integration*](#another-integration) says what holds that open.

Answers #1606. The work is built on `wip/desktop-monitoring` and lands on `main` in one PR.

| Step | Spec | Needs | Status | PR |
| --- | --- | --- | --- | --- |
| 1 | [Detect](1-detect.md) | — | Planned | — |
| 2 | [Serve](2-serve.md) | 1 | Planned | — |
| 3a | [Incidents view](3a-incidents-view.md) | 2 | Planned | — |
| 3b | [Notify](3b-notify.md) | 3c | Planned | — |
| 3c | [Settings](3c-settings.md) | 3a | Planned | — |
| 4 | [Triage](4-triage.md) | 3a | Planned | — |
| 5 | [Automatic triage](5-automatic-triage.md) | 3b, 3c, 4 | Planned | — |

Steps 1 to 3 are a whole feature with no model in it. Steps 4 and 5 each add one thing.

## Decisions

- **A target is one thing the monitor reads.** Each is a row of `monitor_targets` with an `integration`, a `key` and a `label`, and `kubernetes` is the one integration. An integration lists its targets and the core keeps a row for each: a cluster's target is made when its cluster is first listed and deleted when it is listed no more. Incidents, the switch, the read state and every screen name a target, never a cluster.
- **An incident reaches its cluster through its target.** A Kubernetes target's `key` is its cluster's id, which the Kubernetes integration and the GraphQL resolver read as one and the core never does. An incident carries the UID its target had when it opened: for a cluster, the server's. [Step 1](1-detect.md#from-an-incident-to-its-cluster) has the join.
- **An incident is a basic type and its subject's attributes.** `incidents` holds what every incident has: its target and the target's UID, its signal and the signal's label, its subject's key and label, its times, its count, and later its alert and its triage. `subject_attributes` is a JSON object of strings that only the incident's integration writes and the core never reads: for Kubernetes, the subject's API group, kind, namespace and name.
- **Detection is on by default; triage is off by default.** A cluster is read from its local cache and costs nothing. `monitor_targets.enabled` gates detection alone: the user's press and the triage setting gate model spending. No model call is made for monitoring unless the user pressed *Triage with AI* on an incident or turned automatic triage on.
- **Monitoring runs while the app runs.** Closing the last window leaves the app in the tray, and monitoring continues. A quit app watches nothing, and the Monitoring tab says so.
- **An integration lists its targets and takes their readings; the core does the rest.** An integration says which of its signals are true now and which it could not judge, each with a reason from the core's fixed list. The core, which knows no integration, holds the targets' rows, the windows, the incidents, the baseline and the alerts.
- **An integration reads; a connector signs in.** `services/connectors` holds each service's sign-in and its credential. An integration that needs one takes it from its connector, and `app.db` holds none.
- **The signals are a fixed list per integration**, each named with its integration (`kubernetes.crash_loop`) and carrying its label, its severity and its two windows. [Step 1](1-detect.md) lists Kubernetes's six. A signal is a state the integration finds true or not at each reading. An integration that learns of an event reports it as a state, *seen in the last N minutes*: the occurrence lasts those minutes, and a second event inside them adds nothing.
- **An incident is a target under one UID, a subject and a signal.**
  - A subject is its integration's. The core holds three things of it, each written by the integration: a key, which it compares to match an incident and never parses; a label, the one line every screen, alert and brief shows (`Deployment default/web`); and its attributes.
  - A subject is never named by its own UID, so a recreated workload keeps its incident.
  - The subject of a pod's signal is its top controller (Pod → ReplicaSet → Deployment); a pod with no controller is its own subject.
- **A target has one UID at a time**: for a cluster, the UID of the server its context reaches. The monitor stores the UID of its last reading on the target, as `current_uid`. A reading that reaches another ends the target's open occurrences as `stopped` and drops its baseline, as turning monitoring off and on does. An incident belongs to the UID it opened under: a failure on the new server opens a new incident, though its subject has the same name, and so alerts. A cluster recreated under its old name starts over the same way.
- **An incident holds occurrences.** Each signal has two windows: open-after, and resolve-after, which is 5 minutes for each Kubernetes signal. A signal holds until it has been absent for its resolve-after window; a shorter gap does not restart its open-after window. An occurrence starts when the signal has held for its open-after window and resolves when it has been absent for its resolve-after window, at the time it was last seen. A signal that returns within 24 hours of the last occurrence's end adds an occurrence to the same incident. After 24 quiet hours it opens a new incident. An incident keeps its newest 100 occurrences, its true first-seen time and a count of every occurrence it has had.
- **An occurrence ends as `resolved`, `stopped` or `stale`.** `resolved`: its signal was judged absent for the window. `stopped`: the user switched the read off, on the target, the cluster or the kind, or the target's UID changed; it ends at once. `stale`: nothing saw it for 24 hours, so an incident on a target that never answers again does not stay open for good. Each is stamped with the time it was last seen.
- **Only a reading Kubetail can trust opens or resolves anything.** A signal is judged only while every read it depends on answered in full. A cache that is syncing, stale or unreachable, a Kubernetes kind that is failing, an answer cut at its bound: each leaves the signals that depend on it unknown, and their incidents stay as they are. Time the monitor did not watch is unknown too: the app down, the machine asleep. A target is the unit of a partial reading: a signal is unknown for the whole target or for none of it.
- **From step 3b, each signal takes a baseline of a target, over a short window from its first judged reading.** Steps 1 to 3a treat every incident alike. A pair first seen in that window, and still holding when its open-after has passed, starts an occurrence marked `baseline`, with no alert and no automatic triage; its return after recovering is ordinary. The window is the signal's resolve-after, so a failure that flickers out of one reading is still caught. Turning a target's monitoring off, or a new UID, drops its baseline, so the next reading takes a new one. An app restart takes none: a failure that began while the app was closed opens an ordinary incident and alerts.
- **An alert is an OS notification from the host**, with fixed text, the subject's label and the target's label. No model output reaches the OS. On macOS the host asks for notification permission at first launch.
- **What alerts.** With automatic triage off: an incident's first occurrence that is not `baseline`. With it on: an incident whose triage answers `ATTENTION`, or whose triage could not run or failed. A later occurrence of the same incident never alerts on its own.
- **A triage is one monitor run** (`chat.RunMonitor`) on the incident's cluster, in the session the [monitoring session record](../../../apps/desktop/docs/security/2026-10-05-the-monitoring-session.md) describes: read-only, no network, no prompts, no Secret data. Its report opens with a verdict line, `ATTENTION` or `DISMISS`. A report with neither reads as `ATTENTION`. The run checks the target's switch in the transaction that inserts it, through a function the monitor supplies.
- **A dismissed incident stays listed**, with its report.
- **Automatic triage is one setting for every target: a model.** Set, each new incident that is not `baseline` is triaged, and again at its third occurrence. At most 6 automatic triages start per target per hour; an incident past the cap alerts untriaged.
- **Everything a target returns is attacker-controlled text**: names, reasons, messages. It is stored bounded and drawn as text.
- **One package owns the feature in the sidecar**, `internal/agent/monitor`, with the Kubernetes integration in `monitor/kubesignals` and, from step 4, its triage in `monitor/kubetriage`. The core holds the fold, the store and the loop, and imports neither. `kubetriage` calls `agent/chat`, so the layering puts the feature in `agent/`.

## Another integration

No step builds one. These hold the door open, and each is pinned where it is built:

- **The core imports no integration and names no signal.** `monitor.New` takes a registration per integration; `monitor/kubesignals` is the only package that knows a Pod or a cluster. The store and the loop are tested over a fake integration.
- **Nothing in the core keys on a cluster.** Tables, GraphQL types, hooks and the alert name a target. `monitor_targets` has no column for a cluster: the Kubernetes integration lists a target per cluster and reads its key as the cluster's id. `MonitorTarget.clusterID` is resolved from that key for the webview, which joins a target onto its cluster.
- **What only one integration knows goes in the subject's attributes.** Neither `incidents` nor `monitor_targets` has a column that one integration alone fills, or a `CHECK` that lists integrations, so a new integration migrates nothing. Its attribute keys are a stored format all the same, and `persisted-data.md` lists them.
- **Each window is its signal's.** A signal carries open-after and resolve-after, and a resolve-after of zero ends an occurrence at the next judged reading, so a vendor's own verdict is taken as it comes.
- **The read errors are the core's**, a fixed list that holds no integration's own words and already holds `rate_limited`, `unauthenticated` and `unsupported`.
- **A signal is served by its name and its label, not by an enum.** A new signal changes no schema, and a row whose signal this build lacks still draws.
- **The sidecar writes each label, and the webview shows it as it comes**: a target's, a subject's and a signal's. The webview draws the attributes as key–value text. It knows an integration in one place, its label, so a new integration fails the build until it is drawn. A target with no cluster is listed always.

A new integration then adds a package that implements `Integration`, its registration in `runtime.go`, a value of `MonitorIntegration`, and its signals with their labels.

- **Targets the user adds**, with a name, a config and a way to test them. They are rows of a table of the integration's own, which its `ListTargets` lists; every target here is a cluster's. Whether such a target starts enabled is the integration's to say then: a cluster's does because reading it costs nothing.
- **Credentials**: which connector an integration reads with, and what a target shows when it is signed out.
- **Signals the vendor already judged**, such as a cloud alarm: the vendor's start and end times, its rule and alert id, and a severity that differs from one alarm to the next.
- **Triage**, which needs a read-only session over that integration, behind its `Triager`. `chat.RunMonitor` reads a cluster.
- **A target of more than one part**, such as an account's regions. A signal is unknown for a whole target, so such an integration lists a target per part.

## Cut

Each waits for evidence from the MVP.

- `OOMKilled`. It is an event, not a state: a container killed once keeps `lastState.terminated.reason` for good, and the cache holds no time for it, so it cannot be reported as *seen in the last N minutes*. A container killed repeatedly reaches `CrashLoopBackOff`, which is detected.
- Integrations other than Kubernetes: cloud accounts, Cloudflare, hosts. [*Another integration*](#another-integration) says what each would add.
- Signals read from Kubernetes logs or Events.
- A setting for the cap on automatic triages. It is a constant, which the Monitoring tab states.
- Showing what triage costs, for one triage or a day's. Each run's model calls are recorded with their cost in `llm_calls`, so a later change reads it from there.
- Grouping incidents with different subjects under one cause.
- Muting a subject, and acknowledging an incident.
- Turning a report's next steps into a chat draft.
- A model, a brief or an interval the user sets per target.
- A spending budget in dollars.
- Carrying one triage's report into the next.
- Sharing folder grants with the monitor.

## Open question

**How the incidents dialog looks.** Step 3a builds it as a dialog the bell opens, listing every target. The design library has no screen for it yet.

## When it lands

The PR from `wip/desktop-monitoring` into `main`, beyond what each step lists:

- Deletes this folder and closes #1606.
- Replaces `apps/desktop/docs/TODO.md`'s *Build the monitoring agent over `chat.RunMonitor`* with one entry per item under *Cut* worth keeping.
- Writes the desktop ADRs:
  - detect without a model, triage on request;
  - one interface per integration behind a core that knows none;
  - an integration lists its targets, and the core keeps their rows;
  - an incident is a target, a subject and a signal, and holds occurrences;
  - `incidents` holds what every incident has, and the subject's attributes hold the rest;
  - a target holds one UID, and an incident belongs to the UID it opened under;
  - an occurrence ends as resolved, stopped or stale;
  - the host raises an alert with fixed text;
  - `monitor_targets.enabled` gates detection and the triage setting gates unattended runs.
- Adds `apps/desktop/docs/security-model/monitoring.md`, indexed from `security-model.md`, with a row per protection steps 3b, 4 and 5 pin.
