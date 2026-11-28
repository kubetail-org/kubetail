---
title: Raise an alert from the host, with fixed text
date: 2026-11-28
scope: cross-cutting
status: Accepted
---

# Raise an alert from the host, with fixed text

## Context

Monitoring runs while the app runs, and closing the last window leaves the app in the tray, so an
alert must reach the user with no window open. The sidecar decides what alerts; the host owns the
OS integration. A notification draws text outside any window and the OS keeps it in its history,
and the names in it (a workload's, a cluster's) were written by a cluster or the user's kubeconfig.

## Decision

**The sidecar decides and the host shows.** An alert is raised in the transaction that decides it,
by setting `incidents.alerted_at` with an `alert_seq` in commit order. The host subscribes to
`MonitorService.SubscribeAlertEventFeed` over the one host↔sidecar socket and shows each alert
raised since its own launch, at most 5 a group and then a summary. macOS shows it through
`UNUserNotificationCenter`; Windows and Linux through `tauri-plugin-notification`, which no window
may call.

**Its text is fixed around the labels**: title `Kubetail`, body `<subject> on <target>: <signal>`,
each label stripped of control and format characters and cut to its bound, and escaped on Linux. No
model output reaches it, automatic triage included.

## Alternatives considered

- **Notifications from the webview.** There may be no window, and a page able to send them could
  draw any text in the OS's name.
- **The triage's summary in the notification.** It is model output from a model that read the
  cluster, so an injected instruction could choose what the OS shows.
- **An alert raised in memory after the commit.** A restart between the two would lose it, or show
  it twice.

## Consequences

An alert reaches the user from the tray, and its wording cannot be steered by a model. A label within
its bounds is shown as the cluster wrote it, so a workload can be named to read as another's. macOS
serves only a signed app in its bundle, so its prompt, answer and press are checked by hand in a
signed build, and a dev build raises no notification on macOS.

## Revisit when

Users ask for a notification to say what a triage found, which needs output that is not free text.
