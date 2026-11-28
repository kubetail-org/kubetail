# Security record — Monitoring alerts, 27 November 2026

**Subject:** each new incident the monitor finds raises an OS notification, with a window open or
none. The sidecar decides what alerts (`incidents.alerted_at`, set in the transaction that starts
the incident's first ordinary occurrence) and serves the alerts on a gRPC feed,
`MonitorService.SubscribeAlertEventFeed` (`apps/desktop/proto/monitor.proto`). The host listens to
the feed (`src-tauri/src/alerts/`) and shows each alert: through `UNUserNotificationCenter` on
macOS (`alerts/macos.rs`), through `tauri-plugin-notification` on Windows and Linux. The living
model is [security-model.md](../security-model.md).

## What widens

**Text a target returned reaches the OS notification center.** A notification's body names the
subject's label (`Deployment default/web`), the target's label (a cluster's name) and the signal's
label, each of which a cluster or the user's kubeconfig wrote. The OS draws it outside any window
and keeps it in its notification history.

**The host gains a notification plugin and two commands.** `tauri-plugin-notification` is
registered on Windows and Linux. `notification_permission_state_watch` tells an app window whether
the OS shows the notifications, and `notification_press_take` hands it a press.

**A press of a notification opens a window**, on each platform: it surfaces the app window last
focused, else the main window, else any, or opens one, and leaves the press for that window's page
to take. Windows reports a press only while the notification is on screen.

## What holds it

- **No page can send a notification.** Neither capability grants a `notification:` permission
  (`no_window_may_send_a_notification`), so the plugin answers the host alone, and the capability
  lists stay pinned (`default_capability_grants_what_the_app_window_calls`).
- **A Linux body draws no markup.** A notification server may read the body as markup, so the
  host escapes `&`, `<` and `>` in it on Linux, whatever the server says it reads
  (`a_linux_body_escapes_markup_in_labels`).
- **The body is fixed text around the names, each stripped and bounded.** Every control, format,
  separator and default-ignorable character goes, as for a log window's title, and each name is
  cut to its bound: 120 characters for the subject, 60 each for the target and the signal
  (`alert_body_strips_control_characters_and_bounds_each_part`). The sidecar has already bounded
  the subject's label at 256 bytes (`check`).
- **No model output reaches the OS.** Detection uses no model; the notification's text is the
  labels the integration wrote.
- **A notification carries no secret**: the incident's id rides as its identifier, and nothing
  else of the incident.
- **A press names an incident by its id alone.** The host holds it for the window it surfaced, and
  `notification_press_take` hands it only to that window's page, read off the caller's `Webview`
  (`notification_press_take_takes_the_callers_press`). The webview opens the incidents dialog on
  it, which selects nothing for an id it does not hold.
- **The feed rides the one host↔sidecar socket**, whose peer is checked on every dial, as every
  gRPC call's is.

## Residuals

- **A label within its bounds is shown as the cluster wrote it.** A cluster can name a workload so
  its notification reads as another's; the subject's kind and namespace lead the label.
- **The OS keeps the notifications it showed** in its history, beyond the app's 30-day retention of
  the incident.
- **macOS is checked by hand.** The center serves only a signed app in its bundle, so its prompt,
  its answer and a press are verified in a signed build, not by a test. On Windows and Linux, what
  a press means is tested; the plugin's report of a click is checked by hand.
