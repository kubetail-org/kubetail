# Step 3b — Notify

Prerequisite: step 3c on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

A new incident raises an OS notification, with a window open or none. A problem the target already had when monitoring started raises none.

## Design

### Baseline

Monitoring is on by default, so a target's first reading finds whatever was already wrong with it. Those occurrences are marked `baseline` and raise no alert.

- **The baseline is taken per signal, over a window that opens at its first judged reading.** A reading that judges Nodes but not Pods opens the Node signals' window alone; the Pod signals open theirs when Pods are next judged. That reading writes its time under the signal's name in `monitor_targets.baselines`, a JSON object. While the window is open, each pair a reading first sees is marked `baseline` in the fold's memory, beside its totals. A pair so marked starts its occurrence as `baseline`, once it has held for its open-after. A pair first seen after the window closes starts an ordinary occurrence.
- **The window is the signal's resolve-after by the wall clock**, 5 minutes for each Kubernetes signal, from the time in its entry. It runs whether the app is open or not and whether the signal is judged or not: a target added, left after 2 minutes and read again the next day has a closed window, and a failure from the night alerts. A time before the entry's counts as closed, so a clock set back opens no window. One reading is not enough: a failure that flickers, as a crash-looping container does through its `Running` spells, can be missing from it and back at the next. A failure that truly begins inside the window is listed and raises no alert.
- **A restart takes no baseline**: a signal with an entry opens no window again, and the window's end is read from the entry, so a crash loop that began after it alerts whenever the app restarts. A restart inside the window marks again the pairs it sees before the window ends. A pair's mark goes with the pair when it stops holding, so its return later is ordinary.
- **A pair's mark lives in memory until its occurrence starts.** A restart after the window closed loses the mark of each pair still waiting out its open-after, and such a pair starts an ordinary occurrence and alerts. Any signal can be caught so: a crash loop first seen 30 seconds before its window closes is still waiting when it does, and a stretch in which the signal is unknown lengthens the wait.
- **Turning a target's monitoring off, or a reading under a new UID, empties its `baselines`**, in the transaction that ends its open occurrences, and its pairs leave memory with their marks, so the next judged reading takes a new baseline.
- **`Incident` gains `baseline`**, true until the incident has had an ordinary occurrence. It is a column, `incidents.baseline`: set when the incident opens with a `baseline` occurrence, cleared in the transaction that starts its first ordinary one, and never set again. It is not read from the occurrences, which are trimmed to the newest 100: an incident that alerted and then took 100 `baseline` occurrences over later baselines stays ordinary. The incidents list marks a `baseline` incident *Found when monitoring started*, and the bell leaves it out. A baseline failure that recovers and returns within 24 hours adds an ordinary occurrence to the same incident, which then alerts.

Added to `appdb/migrations/0001_init.sql`, as step 1's tables are:

```sql
-- in incident_occurrences
  baseline     INTEGER NOT NULL DEFAULT 0 CHECK (baseline IN (0, 1)),

-- in incidents: 1 until its first ordinary occurrence
  baseline     INTEGER NOT NULL DEFAULT 0 CHECK (baseline IN (0, 1)),

-- in monitor_targets: when each signal's baseline window opened, by signal name
  baselines    TEXT    NOT NULL DEFAULT '{}' CHECK (json_valid(baselines)),
```

### Alerts

**The sidecar decides, the host shows.** The webview takes no part: the app may have no window.

**An alert is raised in the transaction that starts the occurrence**, by setting `incidents.alerted_at`, a new nullable column. This step's rule is the README's with automatic triage off: an incident alerts at its first occurrence that is not `baseline`, the one that clears `incidents.baseline` or opens the incident without it. An incident alerts at most once.

**The host subscribes to an event feed of alerts** on a new gRPC service, `apps/desktop/proto/monitor.proto`. Notifications stay inside the sidecar, so the wire carries the alerts themselves:

```proto
service MonitorService {
  // SubscribeAlertEventFeed sends the retained alerts, newest first, then one
  // Bookmark, then each alert as it is raised.
  rpc SubscribeAlertEventFeed(SubscribeAlertEventFeedRequest) returns (stream AlertEventFeedFrame);
}
```

- **An alert is an incident with `alerted_at` set**, its id the incident's. The feed retains the newest 100, and delivery is promised within them alone: a burst past 100 before the host reads loses the oldest from the feed, never from the bell, which reads incidents.
- **`AlertEventFeedFrame`** is a type, `ALERT` or `BOOKMARK`, and an `Alert` on `ALERT`: the incident's id, its target's id, integration and label, an optional cluster id, the subject's label, the signal's label, the severity and `alerted_at`. Each label is as the sidecar holds it, so the host draws an alert without a list of integrations or signals. The cluster id is filled through `kubesignals.ClusterID` by the gRPC handler, not the core.
- **The sidecar listens on `KeyIncidents` and `KeyMonitorTargets` before it reads**, and on each notice sends the alerts it has not sent on this stream.
- **The host notifies for each alert it has not shown in this process, except those raised before it launched**: it skips an alert whose `alertedAt` is earlier than its own start. An alert the sidecar raises after the host started, but before the host's first snapshot, is shown. The cutoff compares wall-clock times, so an alert raised after a clock set back past the host's start is skipped: its incident is in the bell, and no notification is raised. It keeps the ids it has shown in memory. A reconnect after a sidecar restart starts a new subscription; its snapshot then shows what was raised while the host was away, and nothing twice.
- **The host shows alerts in groups, at most 5 notifications a group, then one, `<n> more incidents`, for the rest.** A snapshot is one group, ended by its `Bookmark`. After it, the first alert to arrive opens a group that takes every alert of the next 2 seconds, a parameter of the listener, however many reads they arrive in. The feed marks no end of a burst, and sends one `Bookmark` only.
- **A disconnect ends the open group, and the host shows it as it stands**: a snapshot cut short before its `Bookmark`, or a live group inside its 2 seconds. An alert is handled once it is in a group, shown or counted in a summary, and the replay after the reconnect skips every handled id.

**The notification** is sent from Rust: on macOS through `UNUserNotificationCenter`, with the `objc2-user-notifications` crate, and on Windows and Linux with `tauri-plugin-notification`. The plugin's macOS side answers `Granted` to every permission question and reports no press, and the center is where macOS keeps both. Its title is `Kubetail`. Its body is fixed text around the names: `<subject label> on <target label>: <signal label>`. Each name and label has its control characters stripped and is cut to a fixed length. The plugin is granted to no window: neither capability file changes.

**Permission.** On macOS the host asks for notification permission once, at the first launch, with the center's `requestAuthorization`, and reads the answer from its `getNotificationSettings`. The center serves only a signed app in its bundle, and asking it from a bare binary raises an exception. So a host running outside a bundle, as `tauri dev` runs it, never calls the center: it sends `NotDetermined` and raises no notification. The answer is a state watch, a new command `notification_permission_state_watch`: it takes a channel, as `log_window_subscribe` does, and sends the value at once and again whenever it changes. The value is `Granted`, `Denied` or `NotDetermined`, the last until the first-launch prompt is answered. Windows and Linux ask nothing and send `Granted`.

- **The host re-reads the answer when a window gains focus.** macOS sends no signal when the user changes it, and the only way to change it is to leave for System Settings and come back, which focuses a window. So focus is the source's change signal, and nothing reads on a timer.
- **The Monitoring tab folds it with `stateWatchReducer`** and, while it is `Denied`, says notifications are off and how to turn them on in System Settings. No frame yet is loading, and the line is not drawn.

**A press** surfaces a window and opens its incidents dialog on the incident, on each platform whose notification reports a press: a window on the alert's cluster. An alert that names no cluster surfaces the window last focused, or opens one. The host hands the incident's id to an open window in an event, and to a window it opens in the window's URL, which the shell reads once. Where none is reported, the bell holds the incident. A notification can outlive what it names. A press whose cluster is gone is treated as one that names no cluster, and a press whose incident is gone opens the dialog with no incident selected, as step 3a's dialog does for any id it does not hold.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Baseline | Planned | — |
| 2. Raise | Planned | — |
| 3. Feed | Planned | — |
| 4. Host | Planned | — |
| 5. Permission | Planned | — |
| 6. Notifications in a packaged build | Planned | — |

1. **Baseline.** The three columns, the pair's mark, the window and the fold's rule with its restart and UID cases, the delete on turning monitoring off, the `baseline` field, the list's mark and the bell's filter.
2. **Raise.** `alerted_at` and the rule, committed before `Notify(appdb.KeyIncidents)`.
3. **Feed.** The proto, `make generate-desktop`, `SubscribeAlertEventFeed` on the monitor service returning its `*Stream`, and the gRPC handler.
4. **Host.** The plugin and the macOS center behind one function, a listener task that holds a clone of the shutdown token and reconnects with the sidecar, the groups with their window as a parameter, the body's text as a tested function, and what a press does on each platform.
5. **Permission.** The first-launch request, the watch and its re-read in the focus handler, the command in `invoke_handler()`, `COMMANDS` and `capabilities/default.json`, and the line in step 3c's tab.
6. **Notifications in a packaged build.** Checked by hand on macOS in a signed build, since no test reaches the center: the first-launch prompt, a notification after *Allow*, none after *Don't Allow* with the tab's line shown, the line gone on returning from System Settings, and a press opening the incident.

## Acceptance

- A signal true at the first reading and still holding after its window opens a `baseline` incident; a Pod `Pending` for 30 seconds at the first reading opens none.
- A restart 1 minute after the first reading, with a crash loop present since it, opens a `baseline` incident; a crash loop that began 6 minutes after a healthy first reading opens an ordinary one, restart or not.
- A container that is `Running` at the first reading and in `CrashLoopBackOff` at the next opens a `baseline` incident and raises no alert (`TestABaselineCatchesAFailureItsFirstReadingMissed`).
- A signal's window closes 5 minutes after its first judged reading, though the signal was unknown for 4 of them.
- A target read for 2 minutes, then again after a day with the app closed, opens an ordinary incident for a crash loop first seen then, and alerts.
- A reading stamped before a signal's entry marks no pair.
- An alert raised after the host started and before its first snapshot raises a notification.
- A stalled rollout first seen in its window, with a restart 8 minutes later, opens an ordinary incident and alerts; with no restart it opens a `baseline` one.
- Turning monitoring off and on again takes a new baseline, and so does a reading under a new UID.
- A subject whose key is the empty string takes its baseline as any other does.
- The list marks a `baseline` incident, and the bell leaves it out.
- An incident whose one ordinary occurrence is trimmed away under 100 later `baseline` ones stays ordinary, and stays in the bell while open.
- An incident that opens with no window open raises one notification.
- A `baseline` occurrence raises none. A second occurrence raises none, but the first ordinary occurrence of a `baseline` incident does.
- A first reading that judges Nodes but not Pods marks the Pod failures found when Pods are next judged as `baseline`.
- 40 alerts raise 5 notifications and one summary, whether they arrive in one read or spread over several inside the window; an alert that arrives after the window opens a new group.
- A marked crash loop still waiting out its open-after at a restart after its window closed opens an ordinary incident and alerts.
- A subject or target named with a newline, an escape sequence or 10,000 bytes yields a one-line body of bounded length (`alert_body_strips_control_characters_and_bounds_each_part`).
- Neither capability grants a notification permission (`no_window_may_send_a_notification`).
- The listener ends on quit and resubscribes after a sidecar restart.
- A disconnect 1 second into a live group of 8 shows 5 notifications and a summary of 3 at once, and a replay of the same 8 ids after the reconnect shows nothing.
- A snapshot cut short before its `Bookmark` shows what arrived, and the next snapshot shows only the ids that did not.
- Pressing a notification whose incident was deleted opens the dialog with no selection; one whose cluster was deleted does so in the window last focused.
- An alert raised while the sidecar restarts is notified once after the host resubscribes; one already shown is not shown again.
- At launch, alerts raised before the host started raise no notification.
- A subscriber gets the retained alerts, newest first, then one `Bookmark`; with none, the `Bookmark` alone.
- With permission denied on macOS, the Monitoring tab says so.
- A host running outside a bundle on macOS makes no call to the center and sends `NotDetermined`.
- `notification_permission_state_watch` sends its value at once. With the tab left open, granting permission in System Settings and returning to the window removes the line, with no reload.
- A focus that finds the answer unchanged sends no frame.

## When it lands

- A security record, `apps/desktop/docs/security/<date>-monitoring-alerts.md`: target-named text reaches the OS notification center, and what bounds it.
- `apps/desktop/CLAUDE.md`'s *Host↔sidecar transport* lists the alert event feed among what gRPC carries.
- `apps/desktop/src-tauri/CLAUDE.md` names the listener, which platform sends through which, the plugin's missing grant, and the permission watch with what re-reads it.
- `apps/desktop/docs/persisted-data.md` gains `alerted_at`, the occurrences' and the incidents' `baseline` and the targets' `baselines`.
- `apps/desktop/sidecar/internal/agent/CLAUDE.md`'s monitor paragraph states the baseline rule.
