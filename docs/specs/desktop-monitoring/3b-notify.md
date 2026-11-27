# Step 3b — Notify

Prerequisite: step 3a on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

A new incident raises an OS notification, with a window open or none. A problem the cluster already had when monitoring started raises none.

## Design

### Baseline

Monitoring is on by default, so a cluster's first reading finds whatever was already wrong with it. Those occurrences are marked `baseline` and raise no alert.

- **The baseline is taken per signal, at its first judged reading.** A reading that judges Nodes but not Pods takes the Node signals' baseline alone; the Pod signals take theirs when Pods are next judged. At that reading, `monitor_baselines` gets a row naming the signal and no subject, which marks its baseline taken, and a row for each of the signal's pairs the reading sees. A baseline pair's occurrence is marked `baseline` once it has held for its window. A pair first seen at a later reading starts an ordinary occurrence.
- **A restart takes no baseline and keeps the one taken**: membership is read from the rows, not inferred from times, so a crash loop that began after the first reading alerts whenever the app restarts. A pair's row goes when the pair stops holding, so its return later is ordinary.
- **Turning a cluster's monitoring off deletes its `monitor_baselines` rows**, so turning it on again takes a new baseline. A context that comes to name another server takes a baseline of that server: the rows carry its `server_uid`.
- **`Incident` gains `baseline`**, true while each of its occurrences is. The incidents list marks a `baseline` incident *Found when monitoring started*, and the bell leaves it out. A baseline failure that recovers and returns within 24 hours adds an ordinary occurrence to the same incident, which then alerts.

Added to `appdb/migrations/0001_init.sql`, as step 1's tables are:

```sql
-- in incident_occurrences
  baseline          INTEGER NOT NULL DEFAULT 0 CHECK (baseline IN (0, 1)),

-- A row with an empty subject marks a signal's baseline taken.
CREATE TABLE monitor_baselines (
  cluster_id        TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
  server_uid        TEXT NOT NULL,
  subject_group     TEXT NOT NULL,
  subject_kind      TEXT NOT NULL,
  subject_namespace TEXT NOT NULL,
  subject_name      TEXT NOT NULL,
  signal            TEXT NOT NULL,
  PRIMARY KEY (cluster_id, server_uid, signal, subject_group, subject_kind, subject_namespace, subject_name)
) STRICT, WITHOUT ROWID;
```

### Alerts

**The sidecar decides, the host shows.** The webview takes no part: the app may have no window.

**An alert is raised in the transaction that starts the occurrence**, by setting `incidents.alerted_at`, a new nullable column. This step's rule is the README's with automatic triage off: an incident alerts at its first occurrence that is not `baseline`. An incident alerts at most once.

**The host subscribes to an event feed of alerts** on a new gRPC service, `apps/desktop/proto/monitor.proto`. Notifications stay inside the sidecar, so the wire carries the alerts themselves:

```proto
service MonitorService {
  // SubscribeAlertEventFeed sends the retained alerts, newest first, then one
  // Bookmark, then each alert as it is raised.
  rpc SubscribeAlertEventFeed(SubscribeAlertEventFeedRequest) returns (stream AlertEventFeedFrame);
}
```

- **An alert is an incident with `alerted_at` set**, its id the incident's. The feed retains the newest 100, and delivery is promised within them alone: a burst past 100 before the host reads loses the oldest from the feed, never from the bell, which reads incidents.
- **`AlertEventFeedFrame`** is a type, `ALERT` or `BOOKMARK`, and an `Alert` on `ALERT`: the incident's id, its cluster's id and display name, the subject's kind, namespace and name, the signal and `alerted_at`.
- **The sidecar listens on `KeyIncidents` and `KeyClusters` before it reads**, and on each notice sends the alerts it has not sent on this stream.
- **The host notifies for each alert it has not shown in this process, except those raised before it launched**: it skips an alert whose `alertedAt` is earlier than its own start. An alert the sidecar raises after the host started, but before the host's first snapshot, is shown. It keeps the ids it has shown in memory. A snapshot or a burst raises at most 5 notifications, then one, `<n> more incidents`, for the rest it received. A reconnect after a sidecar restart starts a new subscription; its snapshot then shows what was raised while the host was away, and nothing twice.

**The notification** is sent from Rust with `tauri-plugin-notification`. Its title is `Kubetail`. Its body is fixed text around the names: `<kind> <namespace>/<name> on <cluster>: <signal label>`. Each name has its control characters stripped and is cut to a fixed length. The plugin is granted to no window: neither capability file changes.

**Permission.** On macOS the host asks for notification permission once, at the first launch. The answer is read by a new command, `notification_permission`, which the Monitoring tab draws: when denied, it says notifications are off and how to turn them on in System Settings. The tab reads it each time it opens. The answer changes only when the user changes it in System Settings, and macOS sends no signal when they do, so nothing watches it: a tab left open shows the answer it opened with. Windows and Linux ask nothing.

**A press** surfaces a window on the incident's cluster at `/incidents?incident=<id>`, on each platform whose notification reports a press. Where none is reported, the bell holds the incident.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Baseline | Planned | — |
| 2. Raise | Planned | — |
| 3. Feed | Planned | — |
| 4. Host | Planned | — |
| 5. Permission | Planned | — |

1. **Baseline.** The table, the column, the fold's rule with its restart case, the delete on turning monitoring off, the `baseline` field, the list's mark and the bell's filter.
2. **Raise.** `alerted_at` and the rule, committed before `Notify(appdb.KeyIncidents)`.
3. **Feed.** The proto, `make generate-desktop`, `SubscribeAlertEventFeed` on the monitor service returning its `*Stream`, and the gRPC handler.
4. **Host.** The plugin, a listener task that holds a clone of the shutdown token and reconnects with the sidecar, the body's text as a tested function, and what a press does on each platform.
5. **Permission.** The first-launch request, the command in `invoke_handler()`, `COMMANDS` and `capabilities/default.json`, and the tab's line.

## Acceptance

- A signal true at the first reading and still holding after its window opens a `baseline` incident; a Pod `Pending` for 30 seconds at the first reading opens none.
- A restart 1 minute after the first reading, with a crash loop present since it, opens a `baseline` incident; a crash loop that began 30 seconds after a healthy first reading opens an ordinary one, restart or not.
- An alert raised after the host started and before its first snapshot raises a notification.
- Turning monitoring off and on again takes a new baseline.
- The list marks a `baseline` incident, and the bell leaves it out.
- An incident that opens with no window open raises one notification.
- A `baseline` occurrence raises none. A second occurrence raises none, but the first ordinary occurrence of a `baseline` incident does.
- A first reading that judges Nodes but not Pods marks the Pod failures found when Pods are next judged as `baseline`.
- 40 alerts raised at once raise 5 notifications and one summary.
- A subject named with a newline, an escape sequence or 10,000 bytes yields a one-line body of bounded length (`alert_body_strips_control_characters_and_bounds_each_name`).
- Neither capability grants a notification permission (`no_window_may_send_a_notification`).
- The listener ends on quit and resubscribes after a sidecar restart.
- An alert raised while the sidecar restarts is notified once after the host resubscribes; one already shown is not shown again.
- At launch, alerts raised before the host started raise no notification.
- A subscriber gets the retained alerts, newest first, then one `Bookmark`; with none, the `Bookmark` alone.
- With permission denied on macOS, the Monitoring tab says so when it opens.

## When it lands

- A security record, `apps/desktop/docs/security/<date>-monitoring-alerts.md`: cluster-named text reaches the OS notification center, and what bounds it.
- `apps/desktop/CLAUDE.md`'s *Host↔sidecar transport* lists the alert event feed among what gRPC carries.
- `apps/desktop/src-tauri/CLAUDE.md` names the listener and the plugin's missing grant.
- `apps/desktop/docs/persisted-data.md` gains `alerted_at`, the occurrences' `baseline` and `monitor_baselines`.
- `apps/desktop/sidecar/internal/agent/CLAUDE.md`'s monitor paragraph states the baseline rule.
