---
title: A log window belongs to one app window, and the host keeps the pair
date: 2026-11-04
scope: cross-cutting
status: Accepted
---

# A log window belongs to one app window, and the host keeps the pair

## Context

A log view was drawn in one place: an app window's right sidebar, sharing width with the chat
([#507](https://github.com/kstackhq/kstack/issues/507)). We wanted a view to leave the sidebar for
a window of its own, while staying tied to the chat that asked for it: its selection rides that
chat's sends, and the app window's cards say where it went.

Every window shares one `localStorage` and the app's events. A page that could name another
window could reach that window's state, and any page holds the whole GraphQL surface. A closing
app window is an event its page may not live to see.

## Decision

**A view moves; it is in one place at a time.** *Open in window* takes it from the sidebar or a
card into a `logs-N` window. The card has one state to draw, and the snapshot has one source.

**The host keeps the pair** (`src-tauri/src/log_windows.rs`). Each log window's entry names its
app window, call, grep, generation, focus order and last viewport push. A page never names a
target: every command starts from the caller's label and checks its window kind beside the
capability (`capabilities/logs.json` grants the viewer and nothing that opens a window, changes a
preference or quits).

**The host talks to an app window over a channel that page made.** `log_window_subscribe` hands
the host a channel and mints a page; the host sends a `snapshot` first, then `opened`, `closed`,
`viewport`, `focused` and `showChat`, each under the lock with the change it reports. Every app
window command names its page, so a page that reloaded changes nothing.

**A log window never outlives its app window.** The host destroys a log window when its app
window is destroyed, and when that window's page closes its log windows on a change of cluster
(`log_window_close_all`). It never `close()`s one, since a page may refuse a close.

**The log window asks the host what to draw** (`log_window_view`), so its URL encodes nothing and a
reload draws the same view. **The host stamps each page load** with a generation, and refuses a
push from an earlier one.

**The selection is pushed, not pulled.** The log window pushes its viewport through the host,
bounded and generation-stamped; the app window keeps a mirror per window, and the composer reads
it at the press. The sidebar's view wins over a window's.

## Alternatives considered

- **Copy the view instead of moving it.** Two live viewers on one call would leave the card two
  states to draw and the snapshot two sources to choose from.
- **Pair the windows in the webview, over events or `localStorage`.** Any page could forge a link
  to another window, emit as the host, or listen to another window's state, and a closing app
  window could not tell its log windows.
- **Encode the view in the log window's URL.** The view is the model's and the cluster's text, and
  a reload would have to trust whatever the URL said.
- **Pull the viewport at send.** `onSend` would wait on another window, and the chip could not show
  a live count.
- **Let a log window outlive its app window.** It would need a detached state to draw, and its
  selection would have no chat to ride.

## Consequences

- The host holds state per window kind, and every new log-window command checks the caller's kind
  and names its page; a test pins each capability's list.
- A send carries the log window's last push, so a change made just before the press can miss it.
- A view's scroll, pause and selection do not move with it: the window opens at the action's
  anchor.
- Another secondary window (exec) can reuse the label prefix, the pair and the channel.

## Revisit when

Windows need to outlive their app window, or to be restored on relaunch.
