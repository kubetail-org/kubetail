# Step 3b — Notify

Prerequisite: step 2 on `wip/desktop-monitoring`. [The sequence's README](README.md) holds the decisions.

A new incident raises an OS notification, with a window open or none.

## Design

**The sidecar decides, the host shows.** The webview takes no part: the app may have no window.

**An alert is raised in the transaction that opens the incident**, by setting `incidents.alerted_at`, a new nullable column. This step's rule is the README's with automatic triage off: an incident's first occurrence alerts unless it is `baseline`. An incident alerts at most once.

**The host listens on a new gRPC service**, `apps/desktop/proto/monitor.proto`:

```proto
service MonitorService {
  // ListenForAlertNotifications streams each alert raised from now on.
  rpc ListenForAlertNotifications(ListenForAlertNotificationsRequest) returns (stream AlertNotification);
}
```

An `AlertNotification` carries the incident's id, its cluster's id and display name, the subject's kind, namespace and name, and the signal. It is a broadcast notification: an alert raised while the host is not listening is not replayed, and the incident is in the app either way.

**The notification** is sent from Rust with `tauri-plugin-notification`. Its title is `Kubetail`. Its body is fixed text around the names: `<kind> <namespace>/<name> on <cluster>: <signal label>`. Each name has its control characters stripped and is cut to a fixed length. The plugin is granted to no window: neither capability file changes.

**Permission.** On macOS the host asks for notification permission once, at the first launch. The answer is read by a new command, `notification_permission`, which the Monitoring tab draws: when denied, it says notifications are off and how to turn them on in System Settings. Windows and Linux ask nothing.

**A press** surfaces a window on the incident's cluster at `/incidents?incident=<id>`, on each platform whose notification reports a press. Where none is reported, the bell holds the incident.

## Tasks

| Task | Status | PR |
| --- | --- | --- |
| 1. Raise | Planned | — |
| 2. Stream | Planned | — |
| 3. Host | Planned | — |
| 4. Permission | Planned | — |

1. **Raise.** `alerted_at`, the rule, and a `gochan` broadcast the stream reads.
2. **Stream.** The proto, `make generate-desktop`, the sidecar's handler.
3. **Host.** The plugin, a listener task that holds a clone of the shutdown token and reconnects with the sidecar, the body's text as a tested function, and what a press does on each platform.
4. **Permission.** The first-launch request, the command in `invoke_handler()`, `COMMANDS` and `capabilities/default.json`, and the tab's line.

## Acceptance

- An incident that opens with no window open raises one notification.
- A `baseline` incident raises none. A second occurrence raises none.
- A subject named with a newline, an escape sequence or 10,000 bytes yields a one-line body of bounded length (`alert_body_strips_control_characters_and_bounds_each_name`).
- Neither capability grants a notification permission (`no_window_may_send_a_notification`).
- The listener ends on quit and resumes after a sidecar restart.
- With permission denied on macOS, the Monitoring tab says so.

## When it lands

- A security record, `apps/desktop/docs/security/<date>-monitoring-alerts.md`: cluster-named text reaches the OS notification center, and what bounds it.
- `apps/desktop/CLAUDE.md`'s *Host↔sidecar transport* lists the alert stream among what gRPC carries.
- `apps/desktop/src-tauri/CLAUDE.md` names the listener and the plugin's missing grant.
- `apps/desktop/docs/persisted-data.md` gains `alerted_at`.
