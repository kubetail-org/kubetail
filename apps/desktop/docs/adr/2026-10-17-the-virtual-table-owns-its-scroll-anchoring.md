---
title: Let the virtual table correct its own scroll position
date: 2026-10-17
scope: frontend
status: Accepted
---

# Let the virtual table correct its own scroll position

## Context

The dashboard's tables (`components/widgets/virtual-table.tsx`) render a live watch: rows are
inserted, removed and reordered on every frame, so the rows a user is reading change index under
a fixed `scrollTop`. Only the rows on screen are in the DOM. The webview is Chromium on Windows
and WebKit on macOS and Linux. The decision predates this file and was recorded in the root
`CLAUDE.md`.

## Decision

`VirtualTable` remembers the rows on screen after each commit. On the next commit it moves
`scrollTop` by the **smallest** shift any surviving row made, so rows moving together stay put and
a row that moved on its own (an event re-fired to the top of a newest-first list) is treated as
the outlier. Within one row of the top it does nothing, so new rows arrive in view like a log
tail. The scroller sets `overflow-anchor: none`.

## Alternatives considered

- **The browser's scroll anchoring.** WebKit has none, so macOS and Linux would get no correction
  at all; Chromium's would stack on top of the table's own and double the shift. Turning it off
  and owning the correction gives every platform the same behaviour.
- **Anchoring on one row** (the first visible). A row that moved by itself would drag the whole
  view with it; the smallest shift ignores it.

## Consequences

The correction depends on every row being `ROW_H` tall and `ROW_H_PX` matching a rendered row;
a wrong constant drifts rows out of place further down a long list.
